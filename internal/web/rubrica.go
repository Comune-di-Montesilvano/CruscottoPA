package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
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
	out := struct {
		OK      bool             `json:"ok"`
		Persone []rubrica.Person `json:"persone"`
	}{Persone: []rubrica.Person{}}
	if s.rubrica != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		people, err := s.rubrica.Search(ctx, r.URL.Query().Get("q"))
		if err != nil {
			if r.Context().Err() == nil { // l'utente ha cambiato testo: non è un guasto
				s.warnRubrica(err)
			}
		} else {
			out.OK, out.Persone = true, people
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
