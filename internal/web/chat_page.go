package web

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

// Interfaccia della chat: widget in plancia, pannello (nel dialog della
// plancia e nella pagina /chat). I messaggi li carica chat.js dalle route
// JSON: nel markup non c'è mai il testo.

const chatWidgetRecent = 5

type chatRecent struct {
	ID    int64
	Con   string
	Nome  string
	Nuovi int
}

type chatView struct {
	Username, Name string
	Verified       bool
	Unread         int
	Recent         []chatRecent // solo per chi ha fatto l'accesso
	Open           string       // collega da aprire (/chat?a=)
	OpenConv       int64        // conversazione da aprire (/chat?c=)
	StatusOn       bool         // «Oggi in chat» attivo (interruttore generale)
	ShowMine       bool         // chi guarda mostra il proprio stato
}

// chatViewFor: nil se la chat non è disponibile per chi guarda.
func (s *Server) chatViewFor(w http.ResponseWriter, r *http.Request) *chatView {
	v, ok := s.chatViewer(w, r)
	if !ok {
		return nil
	}
	cv := &chatView{Username: v.Username, Name: s.chatName(v.Username), Verified: v.Verified}
	if on, _ := s.db.ChatStatusEnabled(); on {
		hidden, _ := s.db.ChatHideStatus(v.Username)
		cv.StatusOn, cv.ShowMine = true, !hidden
	}
	n, err := s.db.ChatUnreadTotal(v.Username)
	if err != nil {
		slog.Warn("chat: non letti", "err", err)
	}
	cv.Unread = n
	if v.Verified {
		cs, err := s.db.ListChatConversations(v.Username, chatWidgetRecent)
		if err != nil {
			slog.Warn("chat: conversazioni", "err", err)
		}
		for _, c := range cs {
			cv.Recent = append(cv.Recent, chatRecent{ID: c.ID, Con: c.Other, Nome: s.chatName(c.Other), Nuovi: c.Unread})
		}
	}
	return cv
}

func (s *Server) handleChatWidget(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.render(w, http.StatusOK, "widget_chat", s.chatViewFor(w, r))
}

type chatPageView struct {
	Chat    *chatView
	Version string
}

func (s *Server) handleChatPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cv := s.chatViewFor(w, r)
	if cv != nil {
		cv.Open = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("a")))
		cv.OpenConv, _ = strconv.ParseInt(r.URL.Query().Get("c"), 10, 64)
	}
	s.render(w, http.StatusOK, "chat.html", chatPageView{Chat: cv, Version: s.version})
}
