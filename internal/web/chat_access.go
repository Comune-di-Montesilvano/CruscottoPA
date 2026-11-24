package web

import (
	"log/slog"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// Chat informale tra colleghi (spec 2026-11-24-chat-interna-design.md).
// Scrivere richiede solo l'identità NTLM (dichiarata); leggere richiede il
// cookie della chat, ottenuto con la password di dominio. Le route rispondono
// sempre 200: il reverse proxy sostituirebbe i 4xx.

// chatEnabled: CHAT_ENABLED e riconoscimento NTLM attivo.
func (s *Server) chatEnabled() bool { return s.cfg.ChatEnabled && s.recognitionEnabled() }

// chatRoutes registra le route pubbliche della chat (solo con chatEnabled;
// quelle dell'admin stanno in routes, dove le controlla TestAuditRoutesCover).
func (s *Server) chatRoutes() {
	s.mux.HandleFunc("POST /chat/accesso", s.handleChatLogin)
	s.mux.HandleFunc("POST /chat/esci", s.handleChatLogout)
	s.mux.HandleFunc("POST /chat/messaggi", s.handleChatSend)
	s.mux.HandleFunc("GET /chat/conversazioni", s.handleChatConversations)
	s.mux.HandleFunc("GET /chat/conversazioni/{id}", s.handleChatThread)
	s.mux.HandleFunc("POST /chat/conversazioni/{id}/letto", s.handleChatRead)
	s.mux.HandleFunc("GET /chat/con/{utente}", s.handleChatWith)
	s.mux.HandleFunc("POST /chat/blocchi", s.handleChatBlock)
	s.mux.HandleFunc("GET /chat/persone", s.handleChatPeople)
	s.mux.HandleFunc("GET /chat/stato", s.handleChatState)
	s.mux.HandleFunc("POST /chat/visibilita", s.handleChatVisibility)
	s.mux.HandleFunc("GET /partials/chat", s.handleChatWidget)
	s.mux.HandleFunc("GET /chat", s.handleChatPage)
}

// chatAllowed: username può scrivere e ricevere messaggi. Serve un profilo AD
// e, se l'admin ha scelto dei gruppi, la regola di visibilità della chat.
// down = AD non raggiungibile (allowed è false).
// chatUsername: collega indicato con lo username (sAMAccountName), mai con
// l'UPN: mrossi@dominio sarebbe un'altra conversazione.
func chatUsername(raw string) (string, bool) {
	u := strings.ToLower(strings.TrimSpace(raw))
	if u == "" || len(u) > 64 {
		return "", false
	}
	for _, r := range u {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return "", false
		}
	}
	return u, true
}

// chatPublic: chat aperta a tutti (nessun gruppo). Chi arriva da AD o dalla
// Rubrica esiste già: non serve leggerne il profilo.
func (s *Server) chatPublic() bool {
	ca, err := s.db.GetContentAudience(database.ContentChat, database.ChatAudienceID)
	return err == nil && len(ca.Groups) == 0
}

// chatAllowedFound: come chatAllowed per un collega appena trovato in AD o
// nella Rubrica.
func (s *Server) chatAllowedFound(username string, public bool) bool {
	if public {
		return username != ""
	}
	ok, _ := s.chatAllowed(username)
	return ok
}

func (s *Server) chatAllowed(username string) (allowed, down bool) {
	if username == "" {
		return false, false
	}
	ca, err := s.db.GetContentAudience(database.ContentChat, database.ChatAudienceID)
	if err != nil {
		slog.Warn("chat: pubblico", "err", err)
		return false, true
	}
	memberOf, known, down := s.groupsFor(username)
	if !known {
		return false, down
	}
	return audience.Visible(ca.Mode, ca.Groups, memberOf, known), false
}
