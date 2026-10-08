// Package otrs apre i ticket in OTRS tramite il GenericInterface REST. È
// l'unico pacchetto che conosce OTRS: host, web service, route, agente e coda
// arrivano dalla configurazione.
package otrs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/mail"
	"path"
	"strings"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

// ErrOTRS: OTRS non ha creato il ticket (irraggiungibile, risposta non JSON
// come la pagina di cortesia del proxy, oppure {"Error":…}).
var ErrOTRS = errors.New("OTRS non disponibile")

// Tempi massimi delle due chiamate: insieme restano sotto il timeout del
// reverse proxy (60 s), altrimenti l'utente vede un errore per un ticket
// che esiste già e lo riapre.
const (
	CreateTimeout = 25 * time.Second
	UpdateTimeout = 10 * time.Second
)

type Client interface {
	Create(ctx context.Context, t NewTicket) (Created, error)
}

type NewTicket struct {
	Name, Email, Phone string
	Subject, Body      string
	Attachments        []Attachment
}

type Attachment struct {
	Filename, ContentType string
	Content               []byte
}

type Created struct {
	TicketID, TicketNumber string
	CustomerSet            bool // false se il TicketUpdate del cliente è fallito
}

// New: client finto con OTRS_URL=mock, altrimenti HTTP.
func New(c config.OTRS) Client {
	if c.Mock() {
		return &Mock{}
	}
	return NewHTTPClient(c)
}

type HTTPClient struct {
	Config        config.OTRS
	HTTP          *http.Client
	CreateTimeout time.Duration // 0 = CreateTimeout
	UpdateTimeout time.Duration // 0 = UpdateTimeout
}

func NewHTTPClient(c config.OTRS) *HTTPClient {
	return &HTTPClient{Config: c, HTTP: &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: 10 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirect non ammesso")
		},
	}}
}

type otrsError struct {
	Error *struct{ ErrorCode, ErrorMessage string } `json:"Error"`
}

// call manda una richiesta JSON e decodifica la risposta in out. Errori
// senza credenziali: la richiesta non compare mai nel messaggio.
func (c *HTTPClient) call(ctx context.Context, method, route string, body map[string]any, out any) error {
	body["UserLogin"] = c.Config.User
	body["Password"] = c.Config.Password
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrOTRS, err)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Config.URL+route, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%w: richiesta non valida", ErrOTRS)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "CruscottoPA")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ue interface{ Unwrap() error }
		if errors.As(err, &ue) && ue.Unwrap() != nil {
			err = ue.Unwrap() // *url.Error: senza ripetere metodo e URL
		}
		slog.Warn("otrs: non raggiungibile", "route", route, "err", err)
		return fmt.Errorf("%w: %v", ErrOTRS, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || resp.StatusCode != http.StatusOK || ct != "application/json" {
		slog.Warn("otrs: risposta non valida", "route", route, "status", resp.StatusCode, "content_type", ct)
		return fmt.Errorf("%w: risposta %d %s", ErrOTRS, resp.StatusCode, ct)
	}
	var e otrsError
	if json.Unmarshal(raw, &e) == nil && e.Error != nil {
		slog.Warn("otrs: errore", "route", route, "code", e.Error.ErrorCode, "message", e.Error.ErrorMessage)
		return fmt.Errorf("%w: %s", ErrOTRS, e.Error.ErrorCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		slog.Warn("otrs: JSON non valido", "route", route)
		return fmt.Errorf("%w: JSON non valido", ErrOTRS)
	}
	return nil
}

func (c *HTTPClient) Create(ctx context.Context, t NewTicket) (Created, error) {
	body := t.Body
	if p := strings.TrimSpace(t.Phone); p != "" {
		body += "\n\nTelefono / interno: " + p
	}
	req := map[string]any{
		"Ticket": map[string]any{
			"Title": t.Subject, "Queue": c.Config.Queue, "State": "new", "Priority": "3 normal",
			"CustomerUser": t.Email,
		},
		"Article": map[string]any{
			"Subject": t.Subject, "Body": body, "ContentType": "text/plain; charset=utf8",
			"ArticleType": "webrequest", "SenderType": "customer",
			"From": (&mail.Address{Name: t.Name, Address: t.Email}).String(),
		},
	}
	if len(t.Attachments) > 0 {
		atts := make([]map[string]any, len(t.Attachments))
		for i, a := range t.Attachments {
			atts[i] = map[string]any{"Filename": safeFilename(a.Filename), "ContentType": a.ContentType,
				"Content": base64.StdEncoding.EncodeToString(a.Content)}
		}
		req["Attachment"] = atts
	}
	var created struct{ TicketID, TicketNumber string }
	cctx, cancel := context.WithTimeout(ctx, orDefault(c.CreateTimeout, CreateTimeout))
	err := c.call(cctx, http.MethodPost, c.Config.RouteCreate, req, &created)
	cancel()
	if err != nil {
		return Created{}, err
	}
	if created.TicketID == "" || created.TicketNumber == "" {
		slog.Warn("otrs: risposta senza ticket")
		return Created{}, fmt.Errorf("%w: risposta senza ticket", ErrOTRS)
	}
	out := Created{TicketID: created.TicketID, TicketNumber: created.TicketNumber, CustomerSet: true}
	// TicketCreate salva il cliente vuoto se la mail non è un customer user di
	// OTRS: si imposta dopo, come fa il PostMaster con i ticket via mail.
	upd := map[string]any{"TicketID": created.TicketID,
		"Ticket": map[string]any{"CustomerUser": t.Email, "CustomerID": t.Email}}
	var ignored map[string]any
	uctx, cancel := context.WithTimeout(ctx, orDefault(c.UpdateTimeout, UpdateTimeout))
	defer cancel()
	if err := c.call(uctx, http.MethodPatch, c.Config.RouteUpdate, upd, &ignored); err != nil {
		slog.Warn("otrs: cliente non impostato", "ticket", created.TicketNumber, "err", err)
		out.CustomerSet = false
	}
	return out, nil
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// safeFilename: solo il nome base (anche da percorsi Windows), max 100 byte.
func safeFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	if name == "." || name == "/" || name == "" {
		return "allegato"
	}
	if len(name) > 100 {
		ext := path.Ext(name)
		if len(ext) > 10 {
			ext = ""
		}
		name = name[:100-len(ext)] + ext
		name = strings.ToValidUTF8(name, "")
	}
	return name
}
