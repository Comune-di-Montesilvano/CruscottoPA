package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

const (
	ticketsPerHour = 5
	maxSubject     = 120
	maxBody        = 10000
	maxPhone       = 40
	// ticketSendTimeout: sotto i 60 s del reverse proxy.
	ticketSendTimeout = 40 * time.Second
)

func (s *Server) ticketsEnabled() bool { return s.tickets != nil }

// ticketRequester: chi apre il ticket, da AD (mai dal form). Identità
// DICHIARATA: le risposte di OTRS vanno comunque alla mail vera.
type ticketRequester struct{ Name, Email, Phone, PC string }

// pcBlock: informazioni sul PC in fondo alla descrizione. Il nome viene dal
// cookie (NTLM), browser, sistema e schermo dal browser: una riga ciascuno.
func pcBlock(pc string, r *http.Request) string {
	if pc == "" {
		pc = "non rilevato"
	}
	b := "\n\n— Informazioni sul PC —\nPC: " + pc
	for _, f := range []struct{ label, field string }{{"Browser", "browser"}, {"Sistema", "sistema"}, {"Schermo", "schermo"}} {
		if v := oneLine(r.FormValue(f.field), 60); v != "" {
			b += "\n" + f.label + ": " + v
		}
	}
	return b
}

// oneLine: testo su una riga (spazi e caratteri di controllo compressi), max n rune.
func oneLine(s string, n int) string {
	s = strings.Join(strings.FieldsFunc(s, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return s
}

func firstAttr(p audience.Profile, name string) string {
	for _, v := range p.Attrs[strings.ToLower(name)] {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// ticketRequester: problem = "anonimo" | "ad" | "mail" se non si può aprire un ticket.
func (s *Server) ticketRequester(r *http.Request) (ticketRequester, string) {
	u, ok := s.viewer(r)
	if !ok || u.Anonymous || u.Username == "" {
		return ticketRequester{}, "anonimo"
	}
	p, ok, down := s.profileFor(u.Username)
	switch {
	case down:
		return ticketRequester{}, "ad"
	case !ok:
		return ticketRequester{}, "anonimo"
	}
	req := ticketRequester{Name: u.Name, Email: firstAttr(p, "mail"), Phone: firstAttr(p, "telephoneNumber"), PC: u.PC}
	if req.Name == "" {
		req.Name = u.Username
	}
	if req.Email == "" {
		return req, "mail"
	}
	return req, ""
}

func (s *Server) handleTicketSend(w http.ResponseWriter, r *http.Request) {
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
	subject := strings.TrimSpace(r.FormValue("oggetto"))
	body := strings.TrimSpace(strings.ReplaceAll(r.FormValue("descrizione"), "\r\n", "\n"))
	phone := strings.TrimSpace(r.FormValue("telefono"))
	if phone == "" {
		phone = req.Phone
	}
	fields := map[string]string{}
	switch n := utf8.RuneCountInString(subject); {
	case n == 0:
		fields["oggetto"] = "Scrivi l'oggetto."
	case n > maxSubject:
		fields["oggetto"] = "Massimo 120 caratteri."
	}
	switch n := utf8.RuneCountInString(body); {
	case n == 0:
		fields["descrizione"] = "Descrivi il problema."
	case n > maxBody:
		fields["descrizione"] = "Massimo 10.000 caratteri."
	}
	if utf8.RuneCountInString(phone) > maxPhone {
		fields["telefono"] = "Massimo 40 caratteri."
	}
	user := s.ticketUser(r)
	// Un invio alla volta per utente: il registro si scrive solo dopo la
	// risposta di OTRS, quindi invii paralleli passerebbero tutti il limite.
	if _, busy := s.ticketSending.LoadOrStore(user, true); busy {
		reply(map[string]any{"errore": "in_corso"})
		return
	}
	defer s.ticketSending.Delete(user)
	n, err := s.db.CountTicketsSince(user, s.now().Add(-time.Hour))
	if err != nil {
		slog.Error("ticket: conteggio", "err", err)
		reply(map[string]any{"errore": "otrs"})
		return
	}
	if n >= ticketsPerHour {
		reply(map[string]any{"errore": "limite"})
		return
	}
	if len(fields) > 0 {
		reply(map[string]any{"campi": fields})
		return
	}
	ids := r.Form["allegato"]
	atts, _, err := s.takeTicketFiles(user, ids)
	if err != nil {
		reply(map[string]any{"campi": map[string]string{"allegati": "Allegati non validi: ricaricali."}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), ticketSendTimeout)
	defer cancel()
	created, err := s.tickets.Create(ctx, otrs.NewTicket{Name: req.Name, Email: req.Email, Phone: phone,
		Subject: subject, Body: body + pcBlock(req.PC, r), Attachments: atts})
	if err != nil {
		s.releaseTicketFiles(ids) // lo stesso dialog può riprovare con gli stessi allegati
		if !errors.Is(err, otrs.ErrOTRS) {
			slog.Error("ticket: invio", "err", err)
		}
		reply(map[string]any{"errore": "otrs"})
		return
	}
	s.consumeTicketFiles(ids)
	if err := s.db.RecordTicket(database.TicketSent{Username: user, Name: req.Name, Email: req.Email, Subject: subject,
		TicketID: created.TicketID, TicketNumber: created.TicketNumber, CustomerSet: created.CustomerSet,
		Attachments: len(atts), PC: req.PC, CreatedAt: s.now()}); err != nil {
		slog.Error("ticket: registro", "ticket", created.TicketNumber, "err", err) // il ticket esiste comunque
	}
	slog.Info("ticket aperto", "ticket", created.TicketNumber, "user", user, "allegati", len(atts))
	reply(map[string]any{"ok": true, "numero": created.TicketNumber, "mail": req.Email})
}
