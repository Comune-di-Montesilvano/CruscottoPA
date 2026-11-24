package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/notify"
)

const chatMaxRunes = 2000

// chatDenied risponde a chi non può usare la chat: "ad" se dipende da AD
// non raggiungibile, altrimenti "anonimo".
func (s *Server) chatDenied(w http.ResponseWriter, r *http.Request) {
	if u, _ := s.viewerName(r); u != "" {
		if _, down := s.chatAllowed(u); down {
			chatFail(w, "ad")
			return
		}
	}
	chatFail(w, "anonimo")
}

// chatName: nome da AD (cache dei profili), altrimenti lo username.
func (s *Server) chatName(username string) string {
	if p, ok, _ := s.profileFor(username); ok {
		if v := p.Attrs["displayname"]; len(v) > 0 && strings.TrimSpace(v[0]) != "" {
			return strings.TrimSpace(v[0])
		}
	}
	return username
}

// handleChatSend: per scrivere basta l'identità NTLM; il messaggio porta
// verified, IP e PC. A un collega che ha bloccato il mittente il messaggio
// non arriva, ma la risposta è identica (il bloccato non lo sa).
func (s *Server) handleChatSend(w http.ResponseWriter, r *http.Request) {
	v, ok := s.chatViewer(w, r)
	if !ok {
		s.chatDenied(w, r)
		return
	}
	text := strings.TrimSpace(r.FormValue("testo"))
	switch {
	case text == "":
		chatFail(w, "vuoto")
		return
	case utf8.RuneCountInString(text) > chatMaxRunes:
		chatFail(w, "lungo")
		return
	}
	to := strings.ToLower(strings.TrimSpace(r.FormValue("a")))
	if to == "" || to == v.Username {
		chatFail(w, "destinatario")
		return
	}
	// Il profilo AD dice anche se il collega esiste.
	if allowed, down := s.chatAllowed(to); down {
		chatFail(w, "ad")
		return
	} else if !allowed {
		chatFail(w, "destinatario")
		return
	}
	now := s.now()
	convID, _, err := s.db.ChatConversationID(v.Username, to, false, now)
	if errors.Is(err, database.ErrNotFound) {
		if !s.chatLimits.allowNewConversation(v.Username, s.dayOf(now)) {
			chatFail(w, "limite")
			return
		}
		convID, _, err = s.db.ChatConversationID(v.Username, to, true, now)
	}
	if err != nil {
		slog.Error("chat: conversazione", "err", err)
		chatFail(w, "errore")
		return
	}
	if !s.chatLimits.allowMessage(v.Username, now) {
		chatFail(w, "limite")
		return
	}
	blocked, err := s.db.ChatBlocked(to, v.Username)
	if err != nil {
		slog.Error("chat: blocchi", "err", err)
		chatFail(w, "errore")
		return
	}
	id, err := s.db.AddChatMessage(database.ChatMessage{ConversationID: convID, Sender: v.Username, Body: text,
		Verified: v.Verified, Hidden: blocked, IP: s.realIP(r), PC: v.PC, CreatedAt: now})
	if err != nil {
		slog.Error("chat: salvataggio del messaggio", "err", err)
		chatFail(w, "errore")
		return
	}
	if !blocked {
		s.notifyChat(to, v.Username, convID, s.chatName(v.Username), v.Verified)
	}
	chatReply(w, map[string]any{"conversazione": convID, "id": id})
}

// notifyChat: evento SSE e push al destinatario, senza il testo (la
// notifica si vede sullo schermo anche con qualcuno accanto). Gli altri
// browser del mittente ricevono un evento senza titolo per aggiornarsi.
func (s *Server) notifyChat(to, from string, convID int64, fromName string, verified bool) {
	title := "Nuovo messaggio da " + fromName
	if !verified {
		title += " (non verificato)"
	}
	url := fmt.Sprintf("/chat?c=%d", convID)
	s.hub.Broadcast(notify.Event{ID: convID, Kind: "chat", Title: title, URL: url}, func(u string) bool { return strings.EqualFold(u, to) })
	s.hub.Broadcast(notify.Event{ID: convID, Kind: "chat", URL: url}, func(u string) bool { return strings.EqualFold(u, from) })
	if s.pusher == nil {
		return
	}
	subs, err := s.db.ListPushSubscriptionsFor(to)
	if err != nil {
		slog.Warn("chat: iscrizioni push", "err", err)
		return
	}
	payload, _ := json.Marshal(map[string]string{"title": title, "body": "", "url": url, "tag": fmt.Sprintf("chat-%d", convID)})
	s.chatPush.Add(1)
	go func() {
		defer s.chatPush.Done()
		for _, sub := range subs {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			gone, err := s.pusher.Send(ctx, sub, payload, false)
			cancel()
			switch {
			case gone:
				s.db.DeletePushSubscription(sub.Endpoint)
			case err != nil:
				slog.Warn("chat: invio push", "err", err)
			}
		}
	}()
}
