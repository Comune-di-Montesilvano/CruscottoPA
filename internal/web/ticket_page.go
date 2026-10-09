package web

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

type ticketPageView struct {
	Ticket    otrs.Ticket
	OK        bool
	Requester ticketRequester
	Version   string
	Fallback  string
}

func (s *Server) handleTicketPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	v := ticketPageView{Version: s.version, Fallback: s.cfg.OTRS.FallbackEmail}
	req, problem := s.ticketRequester(r)
	if !s.ticketsEnabled() || problem != "" {
		s.render(w, http.StatusOK, "ticket.html", v)
		return
	}
	id := r.PathValue("id")
	t, err := s.ticketCache.ticket(req.Email, id, func() (otrs.Ticket, error) {
		ctx, cancel := context.WithTimeout(r.Context(), otrs.ReadTimeout)
		defer cancel()
		return s.tickets.Get(ctx, req.Email, id)
	})
	if err != nil {
		if !errors.Is(err, otrs.ErrNotYours) && !errors.Is(err, otrs.ErrOTRS) {
			slog.Warn("ticket: lettura", "err", err)
		}
		s.render(w, http.StatusOK, "ticket.html", v)
		return
	}
	// Visto fino all'ultimo messaggio mostrato (non "adesso": la pagina può
	// venire dalla cache, e l'orologio di OTRS può differire dal nostro).
	seen := t.Created
	for _, a := range t.Articles {
		if a.Created.After(seen) {
			seen = a.Created
		}
	}
	if err := s.db.MarkTicketSeen(s.ticketUser(r), id, seen); err != nil {
		slog.Warn("ticket: visto", "err", err)
	}
	v.Ticket, v.OK, v.Requester = t, true, req
	s.render(w, http.StatusOK, "ticket.html", v)
}

// attachmentHeaders: immagini in sandbox, PDF come le guide, il resto solo
// come download (mai interpretato dal browser).
func attachmentHeaders(contentType, filename string) http.Header {
	h := http.Header{}
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-store")
	ct, _, _ := mime.ParseMediaType(contentType)
	switch {
	case ct == "image/png" || ct == "image/jpeg" || ct == "image/webp" || ct == "image/gif":
		h.Set("Content-Type", ct)
		h.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	case ct == "application/pdf":
		h.Set("Content-Type", ct)
		h.Set("Content-Security-Policy", "default-src 'none'; object-src 'self'")
	default:
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	}
	return h
}

func (s *Server) handleTicketAttachment(w http.ResponseWriter, r *http.Request) {
	// Sempre 200 con un messaggio: un 404 lo sostituirebbe il proxy.
	text := func(msg string) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte(msg))
	}
	req, problem := s.ticketRequester(r)
	if !s.ticketsEnabled() || problem != "" {
		text("Allegato non disponibile.")
		return
	}
	// Ogni download legge da OTRS tutti gli allegati del ticket: pochi alla volta.
	select {
	case s.ticketDownloads <- struct{}{}:
		defer func() { <-s.ticketDownloads }()
	default:
		text("Troppi download in corso: riprova tra qualche secondo.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), otrs.ReadTimeout)
	defer cancel()
	a, err := s.tickets.Attachment(ctx, req.Email, r.PathValue("id"), r.PathValue("art"), r.PathValue("file"))
	if err != nil {
		text("Allegato non disponibile.")
		return
	}
	for k, vs := range attachmentHeaders(a.ContentType, strings.ReplaceAll(a.Filename, `"`, "")) {
		w.Header()[k] = vs
	}
	w.Write(a.Content)
}
