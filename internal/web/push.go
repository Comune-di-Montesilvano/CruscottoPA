package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/notify"
)

// Il reverse proxy riscrive i 4xx: questi endpoint rispondono sempre 200
// con {"ok":true|false}.
func pushReply(w http.ResponseWriter, ok bool) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]bool{"ok": ok})
}

func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	if s.pusher == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, s.vapidPublic)
}

type pushBody struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func readPushBody(r *http.Request) (pushBody, bool) {
	var b pushBody
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 8<<10)).Decode(&b); err != nil {
		return b, false
	}
	return b, notify.AllowedEndpoint(b.Endpoint)
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	b, ok := readPushBody(r)
	if !ok || s.pusher == nil || b.Keys.P256dh == "" || b.Keys.Auth == "" || len(b.Keys.P256dh) > 200 || len(b.Keys.Auth) > 100 {
		pushReply(w, false)
		return
	}
	username := ""
	if u, ok := s.viewer(r); ok && !u.Anonymous {
		username = u.Username
	}
	err := s.db.SavePushSubscription(database.PushSubscription{Endpoint: b.Endpoint, P256dh: b.Keys.P256dh, Auth: b.Keys.Auth, Username: username, CreatedAt: s.now()})
	pushReply(w, err == nil)
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	b, ok := readPushBody(r)
	if !ok {
		pushReply(w, false)
		return
	}
	pushReply(w, s.db.DeletePushSubscription(b.Endpoint) == nil)
}

// handlePushTest invia una notifica di prova alle iscrizioni dell'admin.
func (s *Server) handlePushTest(w http.ResponseWriter, r *http.Request) {
	msg := ""
	switch subs, err := s.db.ListPushSubscriptionsFor(s.currentAdmin(r)); {
	case s.pusher == nil:
		msg = "Web Push spento: imposta VAPID_SUBJECT."
	case err != nil:
		s.serverError(w, err)
		return
	case len(subs) == 0:
		msg = "Nessuna iscrizione per il tuo utente: apri la plancia da questo PC e attiva le notifiche."
	default:
		payload := notify.Payload(database.Alert{ID: 0, Title: "Notifica di prova", Body: "Le notifiche di CruscottoPA funzionano."})
		sent, failed := 0, 0
		for _, sub := range subs {
			gone, err := s.pusher.Send(r.Context(), sub, payload, false)
			switch {
			case gone:
				s.db.DeletePushSubscription(sub.Endpoint)
				failed++
			case err != nil:
				failed++
			default:
				sent++
			}
		}
		msg = fmt.Sprintf("Iscrizioni: %d · inviate: %d · non riuscite: %d.", len(subs), sent, failed)
	}
	s.render(w, http.StatusOK, "push_test_result", msg)
}

func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, filepath.Join(s.webDir, "static", "sw.js"))
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, filepath.Join(s.webDir, "static", "manifest.webmanifest"))
}
