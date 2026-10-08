package web

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/notify"
)

// Letture degli avvisi e pannello Utenti: dati indicativi (identità
// dichiarata), soggetti all'informativa dell'ente.

type readerRow struct {
	Name, Username, How string
	At                  time.Time
}

type lettureView struct {
	Alert        database.Alert
	Reads        []readerRow
	Deliveries   []database.Delivery
	Unread       []string // utenti attivi che possono vederlo e non l'hanno letto
	UnreadUnsure bool     // AD non disponibile per un avviso riservato: elenco non calcolato
	Trackable    bool     // false: avviso breve non urgente, letto per intero nel carosello
	Names        map[string]string
}

// readTrackable: la lettura si rileva solo se c'è un gesto (popup urgente da
// confermare o testo da espandere); un avviso breve si legge già nel carosello.
func readTrackable(a database.Alert) bool {
	return a.Level == database.LevelUrgent || needsMore(a.Body)
}

// unreadBy: utenti attivi che possono vedere l'avviso e non l'hanno letto.
// Avviso pubblico: nessuna richiesta ad AD. Riservato: regole lette una volta;
// al primo AD non raggiungibile ci si ferma (unsure).
func (s *Server) unreadBy(alertID int64, active []string, read map[string]bool) (users []string, unsure bool, err error) {
	ca, err := s.db.GetContentAudience(database.ContentAlert, alertID)
	if err != nil {
		return nil, false, err
	}
	var rules map[int64][]audience.Rule
	if len(ca.Groups) > 0 {
		if rules, err = s.db.AllAudienceRules(); err != nil {
			return nil, false, err
		}
	}
	for _, u := range active {
		if read[u] {
			continue
		}
		if len(ca.Groups) == 0 {
			users = append(users, u)
			continue
		}
		p, ok, down := s.profileFor(u)
		if down {
			return nil, true, nil
		}
		memberOf := map[int64]bool{}
		if ok {
			for g, rs := range rules {
				if audience.Member(p, rs) {
					memberOf[g] = true
				}
			}
		}
		if audience.Visible(ca.Mode, ca.Groups, memberOf, ok) {
			users = append(users, u)
		}
	}
	return users, false, nil
}

func (s *Server) handleAlertReaders(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := s.db.GetAlert(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	pres, err := s.db.ListPresence()
	if err != nil {
		s.serverError(w, err)
		return
	}
	names := map[string]string{}
	for _, p := range pres {
		names[p.Username] = p.Name
	}
	display := func(u string) string {
		if n := names[u]; n != "" {
			return n
		}
		return u
	}
	reads, err := s.db.ReadsFor(id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	v := lettureView{Alert: a, Names: names, Trackable: readTrackable(a)}
	read := map[string]bool{}
	for _, rd := range reads {
		read[rd.Username] = true
		v.Reads = append(v.Reads, readerRow{Name: display(rd.Username), Username: rd.Username, How: rd.How, At: rd.ReadAt})
	}
	if v.Deliveries, err = s.db.DeliveriesFor(id); err != nil {
		s.serverError(w, err)
		return
	}
	if v.Trackable {
		active, err := s.db.ActiveUsernames(s.now())
		if err != nil {
			s.serverError(w, err)
			return
		}
		users, unsure, err := s.unreadBy(id, active, read)
		if err != nil {
			s.serverError(w, err)
			return
		}
		v.UnreadUnsure = unsure
		for _, u := range users {
			v.Unread = append(v.Unread, display(u))
		}
		slices.Sort(v.Unread)
	}
	s.renderPage(w, r, "admin_letture.html", "avvisi", v)
}

type userRow struct {
	database.Presence
	Active       bool
	Notify       string   // attive | bloccate | da decidere | senza iscrizione
	Services     []string // browser iscritti
	LastReceived *time.Time
}

type utentiView struct {
	Users      []userRow
	OnlyActive bool
	Query      string
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	pres, err := s.db.ListPresence()
	if err != nil {
		s.serverError(w, err)
		return
	}
	subs, err := s.db.ListPushSubscriptions()
	if err != nil {
		s.serverError(w, err)
		return
	}
	last, err := s.db.LastReceivedByUser()
	if err != nil {
		s.serverError(w, err)
		return
	}
	services := map[string][]string{}
	for _, sub := range subs {
		u := strings.ToLower(sub.Username)
		if name := notify.ServiceName(sub.Endpoint); !slices.Contains(services[u], name) {
			services[u] = append(services[u], name)
		}
	}
	v := utentiView{OnlyActive: r.FormValue("attivi") == "1", Query: strings.TrimSpace(r.FormValue("q"))}
	q := strings.ToLower(v.Query)
	cutoff := s.now().Add(-database.ActiveWindow)
	for _, p := range pres {
		row := userRow{Presence: p, Active: !p.LastSeen.Before(cutoff), Services: services[p.Username]}
		if v.OnlyActive && !row.Active {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(p.Username+" "+p.Name), q) {
			continue
		}
		switch {
		case len(row.Services) > 0:
			row.Notify = "attive"
		case p.Permission == "denied":
			row.Notify = "bloccate"
		case p.Permission == "granted":
			row.Notify = "senza iscrizione"
		default:
			row.Notify = "da decidere"
		}
		if t, ok := last[p.Username]; ok {
			row.LastReceived = &t
		}
		v.Users = append(v.Users, row)
	}
	s.renderPage(w, r, "admin_utenti.html", "utenti", v)
}
