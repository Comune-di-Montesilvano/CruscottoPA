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
