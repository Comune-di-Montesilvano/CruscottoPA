package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// sseHeartbeat: commento periodico, perché nginx chiude dopo 60 s di silenzio.
var sseHeartbeat = 25 * time.Second

// handleEvents è il flusso SSE delle plance aperte: arriva un evento quando
// parte la notifica di un avviso visibile a chi guarda.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming non supportato", http.StatusInternalServerError)
		return
	}
	username := ""
	if u, ok := s.viewer(r); ok && !u.Anonymous {
		username = u.Username
	}
	c, err := s.hub.Subscribe(username)
	if err != nil {
		// 200 e "retry": un 503 lo riscriverebbe il proxy e il browser
		// smetterebbe di riprovare. Così si ricollega fra un minuto.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		fmt.Fprint(w, "retry: 60000\n\n")
		return
	}
	defer s.hub.Unsubscribe(c)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // nginx: niente buffer sulla risposta
	fmt.Fprint(w, ": ok\n\n")
	flusher.Flush()

	tick := time.NewTicker(sseHeartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.hub.Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
		case e := <-c.Events:
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "event: avviso\ndata: %s\n\n", b)
		}
		flusher.Flush()
	}
}
