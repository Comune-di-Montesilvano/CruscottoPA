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

const repliesPerHour = 10

func (s *Server) handleTicketReply(w http.ResponseWriter, r *http.Request) {
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
	switch n := utf8.RuneCountInString(body); {
	case n == 0:
		reply(map[string]any{"campi": map[string]string{"descrizione": "Scrivi la risposta."}})
		return
	case n > maxBody:
		reply(map[string]any{"campi": map[string]string{"descrizione": "Massimo 10.000 caratteri."}})
		return
	}
	user := s.ticketUser(r)
	if _, busy := s.ticketSending.LoadOrStore(user, true); busy {
		reply(map[string]any{"errore": "in_corso"})
		return
	}
	defer s.ticketSending.Delete(user)
	n, err := s.db.CountTicketsSince(user, database.TicketReply, s.now().Add(-time.Hour))
	if err != nil {
		slog.Error("ticket: conteggio risposte", "err", err)
		reply(map[string]any{"errore": "otrs"})
		return
	}
	if n >= repliesPerHour {
		reply(map[string]any{"errore": "limite"})
		return
	}
	ids := r.Form["allegato"]
	atts, _, err := s.takeTicketFiles(user, ids)
	if err != nil {
		reply(map[string]any{"campi": map[string]string{"allegati": "Allegati non validi: ricaricali."}})
		return
	}
	id := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), ticketSendTimeout)
	defer cancel()
	err = s.tickets.Reply(ctx, req.Email, id, otrs.NewReply{Name: req.Name, Email: req.Email,
		Body: body + pcBlock(req.PC, s.pcIP(req.PC), r), Attachments: atts})
	if err != nil {
		s.releaseTicketFiles(ids)
		if errors.Is(err, otrs.ErrNotYours) {
			reply(map[string]any{"errore": "non_tuo"})
			return
		}
		reply(map[string]any{"errore": "otrs"})
		return
	}
	s.consumeTicketFiles(ids)
	s.ticketCache.forget(req.Email)
	if err := s.db.RecordTicket(database.TicketSent{Username: user, Name: req.Name, Email: req.Email, Subject: "Risposta",
		TicketID: id, TicketNumber: id, CustomerSet: true, Attachments: len(atts), PC: req.PC, Kind: database.TicketReply,
		CreatedAt: s.now()}); err != nil {
		slog.Error("ticket: registro risposta", "err", err)
	}
	reply(map[string]any{"ok": true})
}
