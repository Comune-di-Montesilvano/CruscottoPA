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
	r.Body = http.MaxBytesReader(w, r.Body, chatMaxBody)
	if err := r.ParseForm(); err != nil {
		chatFail(w, "lungo")
		return
	}
	v, ok := s.chatViewer(w, r)
	if !ok {
		s.chatDenied(w, r)
		return
	}
	// chatViewer con AD giù lascia passare i verificati (per leggere): per
	// scrivere il mittente deve risultare nel pubblico adesso.
	if allowed, down := s.chatAllowed(v.Username); down {
		chatFail(w, "ad")
		return
	} else if !allowed {
		chatFail(w, "anonimo")
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
	to, valid := chatUsername(r.FormValue("a"))
	if !valid || to == v.Username {
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
	// Prima il limite per minuto e ora: un invio rifiutato non crea la
	// conversazione né consuma la quota giornaliera.
	if !s.chatLimits.allowMessage(v.Username, now) {
		chatFail(w, "limite")
		return
	}
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
	// Bloccato: niente al destinatario, ma le altre schede del mittente si
	// aggiornano come sempre (altrimenti se ne accorgerebbe).
	s.notifyChat(to, v.Username, convID, s.chatName(v.Username), v.Verified, !blocked)
	chatReply(w, map[string]any{"conversazione": convID, "id": id})
}

// notifyChat: evento SSE e push al destinatario, senza il testo (la
// notifica si vede sullo schermo anche con qualcuno accanto). Gli altri
// browser del mittente ricevono un evento senza titolo per aggiornarsi.
func (s *Server) notifyChat(to, from string, convID int64, fromName string, verified, toRecipient bool) {
	title := "Nuovo messaggio da " + fromName
	if !verified {
		title += " (non verificato)"
	}
	url := fmt.Sprintf("/chat?c=%d", convID)
	s.hub.Broadcast(notify.Event{ID: convID, Kind: "chat", URL: url}, func(u string) bool { return strings.EqualFold(u, from) })
	if !toRecipient {
		return
	}
	s.hub.Broadcast(notify.Event{ID: convID, Kind: "chat", Title: title, URL: url}, func(u string) bool { return strings.EqualFold(u, to) })
	if s.pusher == nil {
		return
	}
	subs, err := s.db.ListPushSubscriptionsFor(to)
	if err != nil {
		slog.Warn("chat: iscrizioni push", "err", err)
		return
	}
	payload, _ := json.Marshal(map[string]string{"title": title, "body": "", "url": url, "tag": fmt.Sprintf("chat-%d", convID)})
	if !s.chatPushSlot() {
		slog.Warn("chat: troppe push in corso, notifica solo in tempo reale")
		return
	}
	s.chatPush.Add(1)
	go func() {
		defer func() { <-s.chatPushSem; s.chatPush.Done() }()
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

// chatPushMax: invii push della chat in corso insieme (come il dispatcher).
const chatPushMax = 8

// chatMaxBody: corpo di POST /chat/messaggi (2000 rune = al più 8 KB).
const chatMaxBody = 16 << 10

// chatPushSlot prende un posto di invio senza aspettare; chi lo ottiene lo
// libera a fine invio.
func (s *Server) chatPushSlot() bool {
	select {
	case s.chatPushSem <- struct{}{}:
		return true
	default:
		return false
	}
}
