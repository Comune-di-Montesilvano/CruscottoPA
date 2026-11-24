package web

import (
	"log/slog"
	"net/http"
)

// «Oggi in chat»: chi ha usato la chat oggi (aperto il pannello o una
// conversazione, inviato un messaggio). Solo il giorno, mai l'ora; il widget
// che si aggiorna da solo, le notifiche e la ricerca non contano. Chi nasconde
// il proprio stato non vede quello degli altri (scambio alla pari).

// chatTouch segna user come attivo oggi; una scrittura al giorno per utente.
func (s *Server) chatTouch(user string) {
	day := s.dayOf(s.now())
	if prev, ok := s.chatSeen.Load(user); ok && prev == day {
		return
	}
	if err := s.db.TouchChatToday(user, day); err != nil {
		slog.Warn("chat: oggi in chat", "err", err)
		return
	}
	s.chatSeen.Store(user, day)
}

// chatStatusOn: interruttore generale e scelta di chi guarda.
func (s *Server) chatStatusOn(viewer string) (enabled, mine bool) {
	on, err := s.db.ChatStatusEnabled()
	if err != nil || !on {
		return false, false
	}
	hidden, err := s.db.ChatHideStatus(viewer)
	return true, err == nil && !hidden
}

// chatTodayFor: tra users, chi è «oggi in chat» visto da viewer.
func (s *Server) chatTodayFor(viewer string, users []string) map[string]bool {
	if on, mine := s.chatStatusOn(viewer); !on || !mine || len(users) == 0 {
		return map[string]bool{}
	}
	m, err := s.db.ChatToday(users, s.dayOf(s.now()))
	if err != nil {
		slog.Warn("chat: oggi in chat", "err", err)
		return map[string]bool{}
	}
	return m
}

// handleChatVisibility: «Mostra quando sono in chat» (nascondi=1 per nasconderlo).
func (s *Server) handleChatVisibility(w http.ResponseWriter, r *http.Request) {
	v, ok := s.chatViewer(w, r)
	if !ok {
		s.chatDenied(w, r)
		return
	}
	if err := s.db.SetChatHideStatus(v.Username, r.FormValue("nascondi") == "1"); err != nil {
		slog.Error("chat: visibilità dello stato", "err", err)
		chatFail(w, "errore")
		return
	}
	chatReply(w, map[string]any{})
}

func (s *Server) handleAdminChatStatus(w http.ResponseWriter, r *http.Request) {
	if err := s.db.SetChatStatusEnabled(r.FormValue("attivo") == "1"); err != nil {
		s.serverError(w, err)
		return
	}
	auditObject(r, "", "Stato «Oggi in chat»")
	s.renderChatAdminSection(w, r)
}
