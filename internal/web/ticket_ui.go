package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

// widgetTicketRows: righe del widget; l'elenco completo è nella pagina /ticket.
const widgetTicketRows = 5

type ticketRow struct {
	otrs.Summary
	Unread bool
}

type ticketWidget struct {
	Rows  []ticketRow // aperti: prima quelli con una risposta non vista
	Total int         // tutti i ticket dell'utente (aperti e chiusi da poco)
	Down  bool
}

type ticketListView struct {
	Open, Closed []ticketRow
	OK, Down     bool
	Version      string
	Ticket       ticketView // per il dialog «Apri un ticket»
}

// myTickets: ticket dell'utente dalla cache (o da OTRS), con il segno delle
// risposte non viste. Aperti e chiusi separati, i non visti per primi.
func (s *Server) myTickets(r *http.Request, email string) (open, closed []ticketRow, err error) {
	list, err := s.ticketCache.mine(email, func() ([]otrs.Summary, error) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*otrs.ReadTimeout)
		defer cancel()
		return s.tickets.Mine(ctx, email)
	})
	if err != nil {
		if !errors.Is(err, otrs.ErrOTRS) {
			slog.Warn("ticket: elenco", "err", err)
		}
		return nil, nil, err
	}
	seen, err := s.db.TicketSeen(s.ticketUser(r))
	if err != nil {
		slog.Warn("ticket: visti", "err", err)
	}
	for _, t := range list {
		row := ticketRow{Summary: t}
		if !t.LastAgentArticle.IsZero() {
			at, ok := seen[t.TicketID]
			row.Unread = !ok || t.LastAgentArticle.After(at)
		}
		if t.Closed {
			closed = append(closed, row)
		} else {
			open = append(open, row)
		}
	}
	byAttention := func(rows []ticketRow) {
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Unread != rows[j].Unread {
				return rows[i].Unread
			}
			return rows[i].Changed.After(rows[j].Changed)
		})
	}
	byAttention(open)
	byAttention(closed)
	return open, closed, nil
}

// handleTicketWidget: widget caricato a parte (HTMX). Senza modulo, utente
// anonimo o senza mail: niente (200 vuoto).
func (s *Server) handleTicketWidget(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.ticketsEnabled() {
		return
	}
	req, problem := s.ticketRequester(r)
	if problem != "" {
		return
	}
	if err := s.db.UpsertTicketUser(req.Email, s.ticketUser(r), s.now()); err != nil {
		slog.Warn("ticket: mail → utente", "err", err)
	}
	open, closed, err := s.myTickets(r, req.Email)
	if err != nil {
		s.render(w, http.StatusOK, "widget_ticket", ticketWidget{Down: true})
		return
	}
	v := ticketWidget{Rows: open, Total: len(open) + len(closed)}
	if len(v.Rows) > widgetTicketRows {
		v.Rows = v.Rows[:widgetTicketRows]
	}
	s.render(w, http.StatusOK, "widget_ticket", v)
}

// handleTicketList: pagina «I miei ticket», aperti e chiusi da ≤ 7 giorni.
func (s *Server) handleTicketList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	v := ticketListView{Version: s.version, Ticket: s.ticketViewFor(r)}
	req, problem := s.ticketRequester(r)
	if !s.ticketsEnabled() || problem != "" {
		s.render(w, http.StatusOK, "ticket_list.html", v)
		return
	}
	open, closed, err := s.myTickets(r, req.Email)
	v.Open, v.Closed, v.OK, v.Down = open, closed, err == nil, err != nil
	s.render(w, http.StatusOK, "ticket_list.html", v)
}

// ago: quando, in parole («oggi alle 09:30», «ieri», «3 giorni fa»), poi la data.
func ago(t, now time.Time, loc *time.Location) string {
	t, now = t.In(loc), now.In(loc)
	day := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, loc) }
	switch days := int(day(now).Sub(day(t)).Hours()/24 + 0.5); {
	case days <= 0:
		return "oggi alle " + t.Format("15:04")
	case days == 1:
		return "ieri"
	case days < 7:
		return fmt.Sprintf("%d giorni fa", days)
	}
	return t.Format("02/01/2006")
}

// pcSplit: testo di un messaggio e blocco «Informazioni sul PC» separati,
// per mostrare il blocco richiuso invece che in mezzo al testo.
type pcSplit struct{ Text, PC string }

const pcMarker = "— Informazioni sul PC —"

func splitPC(body string) pcSplit {
	i := strings.Index(body, pcMarker)
	if i < 0 {
		return pcSplit{Text: body}
	}
	before := strings.TrimRight(body[:i], "\n")
	rest := strings.TrimLeft(body[i+len(pcMarker):], "\n")
	block, after, _ := strings.Cut(rest, "\n\n")
	text := before
	if after = strings.TrimSpace(after); after != "" {
		text += "\n\n" + after
	}
	return pcSplit{Text: text, PC: strings.TrimSpace(block)}
}

// ticketStep: tappa del tracciato (ricevuto, lavorazione, risolto).
func ticketStep(stateType string) string {
	switch {
	case stateType == "new":
		return "ricevuto"
	case stateType == "closed":
		return "risolto"
	}
	return "lavorazione"
}

// stateClass: classe CSS dello stato (colore della barra e dell'etichetta).
func stateClass(stateType string) string {
	switch {
	case stateType == "new":
		return "st-new"
	case strings.HasPrefix(stateType, "pending"):
		return "st-pending"
	case stateType == "closed":
		return "st-closed"
	}
	return "st-open"
}
