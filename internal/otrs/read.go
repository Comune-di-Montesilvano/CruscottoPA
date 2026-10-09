package otrs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrNotYours: ticket di un altro utente, di un'altra coda o inesistente.
// Il messaggio è uguale per tutti i casi: non dice se il ticket esiste.
var ErrNotYours = errors.New("ticket non disponibile")

const (
	ReadTimeout = 15 * time.Second
	timeLayout  = "2006-01-02 15:04:05"
	maxMine     = 20
	maxChanged  = 50
	closedDays  = 7
)

// maxGetBytes: TicketGet con il contenuto degli allegati (variabile per i test).
var maxGetBytes int64 = 40 << 20

var openStates = []string{"new", "open", "pending reminder", "pending auto"}

// visibleTypes: articoli che il cliente vede anche in OTRS. Note interne,
// *-internal, note-report e articoli di sistema non escono da qui.
var visibleTypes = map[string]bool{"email-external": true, "phone": true, "fax": true, "webrequest": true, "note-external": true}

type Summary struct {
	TicketID, TicketNumber, Title, State, StateType string
	Created, Changed, LastAgentArticle              time.Time
	Closed                                          bool
}

type Ticket struct {
	Summary
	CustomerUserID     string
	Articles           []Article
	AttachmentsOmitted bool // allegati troppo grandi per una risposta: ticket letto senza
}

type Article struct {
	ArticleID           string
	FromAgent           bool
	ToThirdParty        bool // mail dell'operatore a qualcun altro: non è una risposta all'utente
	From, Subject, Body string
	Created             time.Time
	Attachments         []AttachmentInfo
}

type AttachmentInfo struct {
	FileID, Filename, ContentType string
	Size                          int64
}

// flexString accetta "5" e 5: OTRS 5 manda quasi tutto come stringa, non sempre.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexString(n.String())
	return nil
}

// flexList accetta ["5","6"] e "5" (una sola voce).
type flexList []string

func (l *flexList) UnmarshalJSON(b []byte) error {
	var many []flexString
	if err := json.Unmarshal(b, &many); err == nil {
		for _, s := range many {
			*l = append(*l, string(s))
		}
		return nil
	}
	var one flexString
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	if one != "" {
		*l = flexList{string(one)}
	}
	return nil
}

type rawAttachment struct {
	Filename, ContentType, Content flexString
	FilesizeRaw                    flexString
}

type rawArticle struct {
	ArticleID, ArticleType, SenderType, From, To, Subject, Body, Created flexString
	Attachment                                                           []rawAttachment
}

type rawTicket struct {
	TicketID, TicketNumber, Title, State, StateType, Queue, CustomerUserID, Created, Changed flexString
	Article                                                                                  []rawArticle
}

func (c *HTTPClient) search(ctx context.Context, q map[string]any) ([]string, error) {
	q["Queues"] = []string{c.Config.Queue}
	var out struct{ TicketID flexList }
	cctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()
	if err := c.call(cctx, http.MethodPost, c.Config.RouteSearch, q, &out, 1<<20); err != nil {
		return nil, err
	}
	return out.TicketID, nil
}

// getRaw: TicketGet con tutti gli articoli; con allegati solo se servono.
func (c *HTTPClient) getRaw(ctx context.Context, id string, attachments bool) (rawTicket, error) {
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return rawTicket{}, ErrNotYours
	}
	body := map[string]any{"AllArticles": 1, "Attachments": 0}
	if attachments {
		body["Attachments"] = 1
	}
	var out struct{ Ticket []rawTicket }
	cctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()
	// Route con :TicketID (es. /Ticket/:TicketID) oppure senza (es. /TicketGet):
	// in quel caso l'ID viaggia nel corpo, come le credenziali.
	route := c.Config.RouteGet
	if strings.Contains(route, ":TicketID") {
		route = strings.Replace(route, ":TicketID", id, 1)
	} else {
		body["TicketID"] = id
	}
	if err := c.call(cctx, http.MethodGet, route, body, &out, maxGetBytes); err != nil {
		if !errors.Is(err, errTooLarge) && strings.Contains(err.Error(), "TicketGet.") { // es. TicketGet.AccessDenied o ticket inesistente
			return rawTicket{}, ErrNotYours
		}
		return rawTicket{}, err
	}
	if len(out.Ticket) == 0 {
		return rawTicket{}, ErrNotYours
	}
	return out.Ticket[0], nil
}

func (c *HTTPClient) parseTime(s flexString) time.Time {
	t, _ := time.ParseInLocation(timeLayout, string(s), c.loc())
	return t
}

// convert: ticket con i soli articoli visibili al cliente.
func (c *HTTPClient) convert(r rawTicket) Ticket {
	t := Ticket{Summary: Summary{TicketID: string(r.TicketID), TicketNumber: string(r.TicketNumber), Title: string(r.Title),
		State: string(r.State), StateType: string(r.StateType), Created: c.parseTime(r.Created), Changed: c.parseTime(r.Changed),
		Closed: string(r.StateType) == "closed"}, CustomerUserID: string(r.CustomerUserID)}
	for _, a := range r.Article {
		if !visibleTypes[string(a.ArticleType)] || string(a.SenderType) == "system" {
			continue
		}
		art := Article{ArticleID: string(a.ArticleID), FromAgent: string(a.SenderType) == "agent", From: string(a.From),
			ToThirdParty: toThirdParty(a, string(r.CustomerUserID)),
			Subject:      string(a.Subject), Body: string(a.Body), Created: c.parseTime(a.Created)}
		for i, at := range a.Attachment {
			size, _ := strconv.ParseInt(string(at.FilesizeRaw), 10, 64)
			art.Attachments = append(art.Attachments, AttachmentInfo{FileID: strconv.Itoa(i + 1),
				Filename: safeFilename(string(at.Filename)), ContentType: string(at.ContentType), Size: size})
		}
		if art.FromAgent && art.Created.After(t.LastAgentArticle) {
			t.LastAgentArticle = art.Created
		}
		t.Articles = append(t.Articles, art)
	}
	return t
}

// owned: il ticket è nella coda configurata e il cliente è email.
// owned: ticket della coda configurata, del cliente email, non unito ad
// altri né rimosso.
func (c *HTTPClient) owned(r rawTicket, email string) bool {
	if st := string(r.StateType); st == "merged" || st == "removed" {
		return false
	}
	return string(r.Queue) == c.Config.Queue && email != "" && strings.EqualFold(strings.TrimSpace(string(r.CustomerUserID)), strings.TrimSpace(email))
}

// toThirdParty: mail dell'operatore indirizzata ad altri (es. un fornitore).
func toThirdParty(a rawArticle, customer string) bool {
	if string(a.SenderType) != "agent" || string(a.ArticleType) != "email-external" || customer == "" {
		return false
	}
	return !strings.Contains(strings.ToLower(string(a.To)), strings.ToLower(strings.TrimSpace(customer)))
}

func (c *HTTPClient) Mine(ctx context.Context, email string) ([]Summary, error) {
	open, err := c.search(ctx, map[string]any{"CustomerUserLogin": email, "StateType": openStates,
		"SortBy": "Changed", "OrderBy": "Down", "Limit": maxMine})
	if err != nil {
		return nil, err
	}
	since := time.Now().In(c.loc()).AddDate(0, 0, -closedDays).Format(timeLayout)
	closed, err := c.search(ctx, map[string]any{"CustomerUserLogin": email, "StateType": []string{"closed"},
		"TicketCloseTimeNewerDate": since, "SortBy": "Changed", "OrderBy": "Down", "Limit": maxMine})
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, id := range append(open, closed...) {
		if len(out) == maxMine {
			break
		}
		r, err := c.getRaw(ctx, id, false)
		if errors.Is(err, ErrNotYours) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !c.owned(r, email) {
			continue
		}
		out = append(out, c.convert(r).Summary)
	}
	// Aperti prima dei chiusi, poi per ultima modifica.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Closed != out[j].Closed {
			return !out[i].Closed
		}
		return out[i].Changed.After(out[j].Changed)
	})
	return out, nil
}

func (c *HTTPClient) Get(ctx context.Context, email, id string) (Ticket, error) {
	r, err := c.getRaw(ctx, id, true)
	omitted := false
	if errors.Is(err, errTooLarge) {
		// Allegati troppo grandi per una risposta: il testo si legge senza.
		r, err = c.getRaw(ctx, id, false)
		omitted = true
	}
	if err != nil {
		return Ticket{}, err
	}
	if !c.owned(r, email) {
		return Ticket{}, ErrNotYours
	}
	t := c.convert(r)
	t.AttachmentsOmitted = omitted
	return t, nil
}

func (c *HTTPClient) Changed(ctx context.Context, since time.Time) ([]Ticket, error) {
	ids, err := c.search(ctx, map[string]any{"TicketChangeTimeNewerDate": since.In(c.loc()).Format(timeLayout),
		"SortBy": "Changed", "OrderBy": "Down", "Limit": maxChanged})
	if err != nil {
		return nil, err
	}
	// Un ticket illeggibile non blocca gli altri: si restituiscono quelli letti
	// insieme al primo errore (chi chiama non deve considerare il giro completo).
	out := []Ticket{}
	var firstErr error
	for _, id := range ids {
		r, err := c.getRaw(ctx, id, false)
		if errors.Is(err, ErrNotYours) {
			continue
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if string(r.Queue) != c.Config.Queue {
			continue
		}
		out = append(out, c.convert(r))
	}
	return out, firstErr
}

func (c *HTTPClient) Attachment(ctx context.Context, email, id, articleID, fileID string) (Attachment, error) {
	r, err := c.getRaw(ctx, id, true)
	if err != nil {
		return Attachment{}, err
	}
	if !c.owned(r, email) {
		return Attachment{}, ErrNotYours
	}
	for _, a := range r.Article {
		if string(a.ArticleID) != articleID || !visibleTypes[string(a.ArticleType)] || string(a.SenderType) == "system" {
			continue
		}
		n, err := strconv.Atoi(fileID)
		if err != nil || n < 1 || n > len(a.Attachment) {
			return Attachment{}, ErrNotYours
		}
		at := a.Attachment[n-1]
		data, err := base64.StdEncoding.DecodeString(string(at.Content))
		if err != nil {
			return Attachment{}, fmt.Errorf("%w: allegato non valido", ErrOTRS)
		}
		return Attachment{Filename: safeFilename(string(at.Filename)), ContentType: string(at.ContentType), Content: data}, nil
	}
	return Attachment{}, ErrNotYours
}

type NewReply struct {
	Name, Email, Body string
	Attachments       []Attachment
}

// Reply aggiunge un messaggio del cliente; un ticket chiuso torna "open".
func (c *HTTPClient) Reply(ctx context.Context, email, id string, rep NewReply) error {
	r, err := c.getRaw(ctx, id, false)
	if err != nil {
		return err
	}
	if !c.owned(r, email) {
		return ErrNotYours
	}
	upd := map[string]any{"TicketID": string(r.TicketID), "Article": map[string]any{
		"Subject": "Re: " + string(r.Title), "Body": rep.Body, "ContentType": "text/plain; charset=utf8",
		"ArticleType": "webrequest", "SenderType": "customer",
		"From": (&mail.Address{Name: rep.Name, Address: rep.Email}).String(),
	}}
	if string(r.StateType) == "closed" {
		upd["Ticket"] = map[string]any{"State": "open"}
	}
	if len(rep.Attachments) > 0 {
		atts := make([]map[string]any, len(rep.Attachments))
		for i, a := range rep.Attachments {
			atts[i] = map[string]any{"Filename": safeFilename(a.Filename), "ContentType": a.ContentType,
				"Content": base64.StdEncoding.EncodeToString(a.Content)}
		}
		upd["Attachment"] = atts
	}
	var ignored map[string]any
	cctx, cancel := context.WithTimeout(ctx, orDefault(c.CreateTimeout, CreateTimeout))
	defer cancel()
	return c.call(cctx, http.MethodPatch, c.Config.RouteUpdate, upd, &ignored, 1<<20)
}
