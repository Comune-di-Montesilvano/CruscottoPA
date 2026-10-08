package web

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

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
	UnreadUnsure bool     // AD non disponibile per un avviso riservato
	Names        map[string]string
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
	v := lettureView{Alert: a, Names: names}
	read := map[string]bool{}
	for _, rd := range reads {
		read[rd.Username] = true
		v.Reads = append(v.Reads, readerRow{Name: display(rd.Username), Username: rd.Username, How: rd.How, At: rd.ReadAt})
	}
	if v.Deliveries, err = s.db.DeliveriesFor(id); err != nil {
		s.serverError(w, err)
		return
	}
	active, err := s.db.ActiveUsernames(s.now())
	if err != nil {
		s.serverError(w, err)
		return
	}
	for _, u := range active {
		if read[u] {
			continue
		}
		visible, unsure := s.alertVisibleTo(u, id)
		if unsure {
			v.UnreadUnsure = true
		}
		if visible {
			v.Unread = append(v.Unread, display(u))
		}
	}
	slices.Sort(v.Unread)
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
