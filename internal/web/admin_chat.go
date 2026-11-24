package web

import (
	"net/http"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// Sezione admin "Chat": solo chi può usarla (gruppi della plancia). L'admin
// non vede le conversazioni.

type chatAdminSection struct {
	Visibility visibilityField
	Errors     formErrors
	Saved      bool
	Stats      *chatAdminStats // solo nella pagina (il salvataggio rende la sezione)
}

// chatAdminStats: numeri aggregati, mai nomi né orari.
type chatAdminStats struct {
	database.ChatStats
	UnverifiedPct int // % dei messaggi degli ultimi 30 giorni scritti senza accesso
}

func (s *Server) chatAdminData(ca database.ContentAudience, errs formErrors) (chatAdminSection, error) {
	f, err := s.visibilityField(ca)
	f.Chat = true
	return chatAdminSection{Visibility: f, Errors: errs}, err
}

func (s *Server) handleAdminChatPage(w http.ResponseWriter, r *http.Request) {
	ca, err := s.db.GetContentAudience(database.ContentChat, database.ChatAudienceID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	sec, err := s.chatAdminData(ca, nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	st, err := s.db.ChatStats(s.now())
	if err != nil {
		s.serverError(w, err)
		return
	}
	sec.Stats = &chatAdminStats{ChatStats: st}
	if st.Messages30 > 0 {
		sec.Stats.UnverifiedPct = (st.Unverified30*100 + st.Messages30/2) / st.Messages30
	}
	s.renderPage(w, r, "admin_chat.html", "chat", sec)
}

func (s *Server) handleAdminChatSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Richiesta non valida", http.StatusBadRequest)
		return
	}
	errs := formErrors{}
	ca := parseVisibility(r, errs)
	if len(errs) == 0 {
		if err := s.checkGroups(ca, errs); err != nil {
			s.serverError(w, err)
			return
		}
	}
	status := http.StatusOK
	if len(errs) > 0 {
		status = http.StatusUnprocessableEntity
	} else if err := s.db.SetContentAudience(database.ContentChat, database.ChatAudienceID, ca); err != nil {
		s.serverError(w, err)
		return
	} else {
		auditObject(r, "", "Chi può usare la chat")
	}
	sec, err := s.chatAdminData(ca, errs)
	if err != nil {
		s.serverError(w, err)
		return
	}
	sec.Saved = len(errs) == 0
	s.render(w, status, "chat_admin_section", sec)
}
