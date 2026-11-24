package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// Lettura della chat: solo per chi ha fatto l'accesso (Verified). Le risposte
// non portano mai IP né PC dei messaggi.

const (
	chatPageSize      = 50
	chatListSize      = 30
	chatPeopleMax     = 8
	chatPeopleMinRune = 2
)

// chatReader: viewer verificato, altrimenti risponde e restituisce false.
func (s *Server) chatReader(w http.ResponseWriter, r *http.Request) (chatViewer, bool) {
	v, ok := s.chatViewer(w, r)
	if !ok {
		s.chatDenied(w, r)
		return v, false
	}
	if !v.Verified {
		chatFail(w, "accesso")
		return v, false
	}
	return v, true
}

func chatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func (s *Server) handleChatConversations(w http.ResponseWriter, r *http.Request) {
	v, ok := s.chatReader(w, r)
	if !ok {
		return
	}
	s.chatTouch(v.Username)
	cs, err := s.db.ListChatConversations(v.Username, chatListSize)
	if err != nil {
		slog.Error("chat: conversazioni", "err", err)
		chatFail(w, "errore")
		return
	}
	others := make([]string, len(cs))
	for i, c := range cs {
		others[i] = c.Other
	}
	today := s.chatTodayFor(v.Username, others)
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		out = append(out, map[string]any{"id": c.ID, "con": c.Other, "nome": s.chatName(c.Other), "dettaglio": s.chatDetail(c.Other),
			"ultimo": chatTime(c.LastAt), "nuovi": c.Unread, "oggi": today[c.Other]})
	}
	chatReply(w, map[string]any{"conversazioni": out})
}

// chatThread: messaggi visibili a v prima di before (0 = più recenti).
func (s *Server) chatThread(v chatViewer, convID int64, other string, before int64) (map[string]any, error) {
	s.chatTouch(v.Username)
	out := map[string]any{"id": convID, "con": other, "nome": s.chatName(other), "dettaglio": s.chatDetail(other), "accesso": true,
		"oggi": s.chatTodayFor(v.Username, []string{other})[other], "notifiche": s.chatNotifyState(v.Username, other),
		"messaggi": []map[string]any{}, "altri": false}
	blocked, err := s.db.ChatBlocked(v.Username, other)
	if err != nil {
		return nil, err
	}
	out["bloccato"] = blocked
	if convID == 0 {
		return out, nil
	}
	ms, err := s.db.ChatMessages(convID, v.Username, before, chatPageSize+1)
	if err != nil {
		return nil, err
	}
	if len(ms) > chatPageSize {
		out["altri"] = true
		ms = ms[1:]
	}
	list := make([]map[string]any, 0, len(ms))
	for _, m := range ms {
		list = append(list, map[string]any{"id": m.ID, "mio": m.Sender == v.Username, "testo": m.Body,
			"verificato": m.Verified, "quando": chatTime(m.CreatedAt)})
	}
	out["messaggi"] = list
	return out, nil
}

func (s *Server) handleChatThread(w http.ResponseWriter, r *http.Request) {
	v, ok := s.chatReader(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		chatFail(w, "conversazione")
		return
	}
	other, err := s.db.ChatConversationOther(id, v.Username)
	if errors.Is(err, database.ErrNotFound) {
		chatFail(w, "conversazione")
		return
	} else if err != nil {
		slog.Error("chat: conversazione", "err", err)
		chatFail(w, "errore")
		return
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("prima"), 10, 64)
	out, err := s.chatThread(v, id, other, before)
	if errors.Is(err, database.ErrNotFound) {
		chatFail(w, "conversazione")
		return
	} else if err != nil {
		slog.Error("chat: messaggi", "err", err)
		chatFail(w, "errore")
		return
	}
	chatReply(w, out)
}

// handleChatWith: conversazione con un collega. Anche senza accesso risponde
// con il nome (per scrivere il primo messaggio), ma senza messaggi.
func (s *Server) handleChatWith(w http.ResponseWriter, r *http.Request) {
	v, ok := s.chatViewer(w, r)
	if !ok {
		s.chatDenied(w, r)
		return
	}
	other, ok := chatUsername(r.PathValue("utente"))
	if !ok || other == v.Username {
		chatFail(w, "destinatario")
		return
	}
	if allowed, down := s.chatAllowed(other); !allowed {
		// Con AD giù chi legge vede ancora le conversazioni esistenti.
		switch {
		case down && v.Verified:
		case down:
			chatFail(w, "ad")
			return
		default:
			chatFail(w, "destinatario")
			return
		}
	}
	if !v.Verified {
		chatReply(w, map[string]any{"id": 0, "con": other, "nome": s.chatName(other), "dettaglio": s.chatDetail(other), "accesso": false,
			"oggi": s.chatTodayFor(v.Username, []string{other})[other], "notifiche": s.chatNotifyState(v.Username, other),
			"messaggi": []map[string]any{}, "altri": false, "bloccato": false})
		return
	}
	id, _, err := s.db.ChatConversationID(v.Username, other, false, s.now())
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		slog.Error("chat: conversazione", "err", err)
		chatFail(w, "errore")
		return
	}
	out, err := s.chatThread(v, id, other, 0)
	if err != nil {
		slog.Error("chat: messaggi", "err", err)
		chatFail(w, "errore")
		return
	}
	chatReply(w, out)
}

func (s *Server) handleChatRead(w http.ResponseWriter, r *http.Request) {
	v, ok := s.chatReader(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		chatFail(w, "conversazione")
		return
	}
	if err := s.db.MarkChatRead(v.Username, id); err != nil {
		slog.Error("chat: letto", "err", err)
		chatFail(w, "errore")
		return
	}
	chatReply(w, map[string]any{})
}

func (s *Server) handleChatBlock(w http.ResponseWriter, r *http.Request) {
	v, ok := s.chatReader(w, r)
	if !ok {
		return
	}
	other, ok := chatUsername(r.FormValue("utente"))
	if !ok || other == v.Username {
		chatFail(w, "destinatario")
		return
	}
	if err := s.db.SetChatBlock(v.Username, other, r.FormValue("blocca") == "1", s.now()); err != nil {
		slog.Error("chat: blocco", "err", err)
		chatFail(w, "errore")
		return
	}
	chatReply(w, map[string]any{})
}

// handleChatPeople: colleghi a cui si può scrivere (anche senza accesso). Il
// testo cercato non va mai nei log.
func (s *Server) handleChatPeople(w http.ResponseWriter, r *http.Request) {
	v, ok := s.chatViewer(w, r)
	if !ok {
		s.chatDenied(w, r)
		return
	}
	out := []map[string]any{}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) >= chatPeopleMinRune && s.directory != nil {
		people, err := s.directory.SearchUsers(q)
		if err != nil {
			slog.Warn("chat: ricerca colleghi", "err", err)
			chatFail(w, "ad")
			return
		}
		public := s.chatPublic()
		// Con un pubblico ristretto ogni candidato è un profilo da AD: al più
		// il doppio dei posti.
		for i, p := range people {
			if len(out) >= chatPeopleMax || i >= 2*chatPeopleMax {
				break
			}
			u := strings.ToLower(p.Username)
			if u == v.Username || !s.chatAllowedFound(u, public) {
				continue
			}
			name := p.Name
			if name == "" {
				name = u
			}
			detail := p.Office
			if detail == "" {
				detail = u
			}
			out = append(out, map[string]any{"utente": u, "nome": name, "dettaglio": detail})
		}
		users := make([]string, len(out))
		for i, p := range out {
			users[i] = p["utente"].(string)
		}
		today := s.chatTodayFor(v.Username, users)
		for _, p := range out {
			p["oggi"] = today[p["utente"].(string)]
		}
	}
	chatReply(w, map[string]any{"persone": out})
}

// handleChatState: solo il numero di messaggi nuovi (niente nomi né testo),
// anche per chi non ha fatto l'accesso.
func (s *Server) handleChatState(w http.ResponseWriter, r *http.Request) {
	v, ok := s.chatViewer(w, r)
	if !ok {
		s.chatDenied(w, r)
		return
	}
	n, err := s.db.ChatUnreadTotal(v.Username)
	if err != nil {
		slog.Error("chat: non letti", "err", err)
		chatFail(w, "errore")
		return
	}
	chatReply(w, map[string]any{"verificato": v.Verified, "nuovi": n})
}
