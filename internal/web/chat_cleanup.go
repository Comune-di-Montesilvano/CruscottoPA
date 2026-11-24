package web

import (
	"errors"
	"log/slog"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// chatRetention: i messaggi della chat si cancellano dopo 30 giorni.
const chatRetention = 30 * 24 * time.Hour

// cleanupChat (giro giornaliero): messaggi oltre 30 giorni, anche a chat
// spenta; poi conversazioni e blocchi di chi AD non conosce più. Con AD non
// raggiungibile gli utenti restano fino al prossimo giro.
func (s *Server) cleanupChat() {
	if err := s.db.CleanupChat(s.now().Add(-chatRetention)); err != nil {
		slog.Warn("pulizia chat", "err", err)
		return
	}
	if err := s.db.CleanupChatToday(s.dayOf(s.now())); err != nil {
		slog.Warn("pulizia chat", "err", err)
	}
	if s.directory == nil {
		return
	}
	users, err := s.db.ChatUsers()
	if err != nil {
		slog.Warn("pulizia chat", "err", err)
		return
	}
	for _, u := range users {
		_, err := s.directory.Lookup(u)
		switch {
		case errors.Is(err, identity.ErrUnknownUser):
			if err := s.db.DeleteChatUser(u); err != nil {
				slog.Warn("pulizia chat", "err", err)
			}
		case err != nil:
			return
		}
	}
}
