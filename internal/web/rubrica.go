package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/rubrica"
)

// rubricaWarnEvery: con la Rubrica giù, un avviso nel log al massimo ogni minuto.
const rubricaWarnEvery = time.Minute

type rubricaWarn struct {
	mu   sync.Mutex
	last time.Time
}

// handleRubricaSearch: GET /rubrica/cerca?q= — contatti della Rubrica per il
// gruppo «Persone» della ricerca a tendina. Sempre 200 (il proxy sostituirebbe
// gli errori); spenta o Rubrica giù = {"ok":false,"persone":[]}. Il testo
// cercato non va mai nei log.
func (s *Server) handleRubricaSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	type person struct {
		rubrica.Person
		Chat bool `json:"chat"` // si può scrivergli in chat
	}
	out := struct {
		OK      bool     `json:"ok"`
		Persone []person `json:"persone"`
	}{Persone: []person{}}
	if s.rubrica != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		people, err := s.rubrica.Search(ctx, r.URL.Query().Get("q"))
		if err != nil {
			if r.Context().Err() == nil { // l'utente ha cambiato testo: non è un guasto
				s.warnRubrica(err)
			}
		} else {
			out.OK = true
			// La Rubrica ha anche contatti fuori da AD: qui il profilo serve
			// sempre (al più 8 risultati, profili in cache).
			_, canChat := s.chatViewer(w, r)
			for _, p := range people {
				pp := person{Person: p}
				if canChat && p.UID != "" {
					pp.Chat, _ = s.chatAllowed(strings.ToLower(p.UID))
				}
				out.Persone = append(out.Persone, pp)
			}
		}
	}
	json.NewEncoder(w).Encode(out)
}

func (s *Server) warnRubrica(err error) {
	s.rubricaWarn.mu.Lock()
	defer s.rubricaWarn.mu.Unlock()
	now := s.now()
	if !s.rubricaWarn.last.IsZero() && now.Sub(s.rubricaWarn.last) < rubricaWarnEvery {
		return
	}
	s.rubricaWarn.last = now
	slog.Warn("rubrica non raggiungibile", "err", err)
}
