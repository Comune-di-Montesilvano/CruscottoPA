package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

// handleTicketClose: l'utente chiude un proprio ticket, con un testo
// facoltativo. Stesse regole della risposta: sempre 200 + JSON, un invio
// alla volta, limite orario.
func (s *Server) handleTicketClose(w http.ResponseWriter, r *http.Request) {
	reply := func(v map[string]any) {
		if v["ok"] != true {
			v["ok"] = false
			if c := s.cfg.OTRS.FallbackEmail; c != "" {
				v["casella"] = c
			}
		}
		mediaJSON(w, v)
	}
	if !s.ticketsEnabled() {
		reply(map[string]any{"errore": "spento"})
		return
	}
	req, problem := s.ticketRequester(r)
	if problem != "" {
		reply(map[string]any{"errore": problem})
		return
	}
	body := strings.TrimSpace(strings.ReplaceAll(r.FormValue("descrizione"), "\r\n", "\n"))
	if utf8.RuneCountInString(body) > maxBody {
		reply(map[string]any{"campi": map[string]string{"descrizione": "Massimo 10.000 caratteri."}})
		return
	}
	user := s.ticketUser(r)
	if _, busy := s.ticketSending.LoadOrStore(user, true); busy {
		reply(map[string]any{"errore": "in_corso"})
		return
	}
	defer s.ticketSending.Delete(user)
	n, err := s.db.CountTicketsSince(user, database.TicketClose, s.now().Add(-time.Hour))
	if err != nil {
		slog.Error("ticket: conteggio chiusure", "err", err)
		reply(map[string]any{"errore": "otrs"})
		return
	}
	if n >= repliesPerHour {
		reply(map[string]any{"errore": "limite"})
		return
	}
	id := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), ticketSendTimeout)
	defer cancel()
	err = s.tickets.Close(ctx, req.Email, id, otrs.NewClose{Name: req.Name, Email: req.Email, Body: body})
	if err != nil {
		if errors.Is(err, otrs.ErrNotYours) {
			reply(map[string]any{"errore": "non_tuo"})
			return
		}
		if errors.Is(err, otrs.ErrDenied) {
			reply(map[string]any{"errore": "permesso"})
			return
		}
		reply(map[string]any{"errore": "otrs"})
		return
	}
	s.ticketCache.forget(req.Email)
	slog.Info("ticket: chiuso dall'utente", "ticket", id, "user", user, "testo", body != "")
	if err := s.db.RecordTicket(database.TicketSent{Username: user, Name: req.Name, Email: req.Email, Subject: "Chiusura",
		TicketID: id, TicketNumber: id, CustomerSet: true, PC: req.PC, Kind: database.TicketClose,
		CreatedAt: s.now()}); err != nil {
		slog.Error("ticket: registro chiusura", "err", err)
	}
	reply(map[string]any{"ok": true})
}
