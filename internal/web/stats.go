package web

import (
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// «Noi del Comune»: numeri di gruppo e traguardi personali in plancia. Mai
// classifiche tra persone né orari: dei giorni di accesso si salva solo il
// giorno. Dati indicativi (identità dichiarata), solo per personalizzare.

const (
	pillTTL        = time.Minute
	milestoneEvery = time.Minute
)

// statsCache: numeri di gruppo letti dal DB al massimo una volta per TTL.
type statsCache struct {
	mu          sync.Mutex
	pill        pillView
	pillDay     string
	pillAt      time.Time
	milestoneAt time.Time
}

// reset svuota la cache (test).
func (c *statsCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pill, c.pillDay, c.pillAt, c.milestoneAt = pillView{}, "", time.Time{}, time.Time{}
}

// pillView: «Oggi siamo in N» nella testata; Count 0 = nascosta. Con un
// traguardo raggiunto oggi vale quello.
type pillView struct {
	Count     int
	Milestone int64
}

// dayOf: giorno di calendario dell'ente (YYYY-MM-DD).
func (s *Server) dayOf(t time.Time) string { return t.In(s.loc()).Format(database.DayLayout) }

// recordAccess segna l'accesso del giorno e, al massimo una volta al minuto,
// controlla i traguardi collettivi.
func (s *Server) recordAccess(username string) {
	now := s.now()
	day := s.dayOf(now)
	added, err := s.db.RecordDay(username, day)
	if err != nil {
		slog.Warn("statistiche", "err", err)
		return
	}
	if !added {
		return
	}
	s.stats.mu.Lock()
	s.stats.pillAt = time.Time{} // un collega in più: la pastiglia si ricalcola
	due := now.Sub(s.stats.milestoneAt) >= milestoneEvery
	if due {
		s.stats.milestoneAt = now
	}
	s.stats.mu.Unlock()
	if !due {
		return
	}
	total, err := s.db.AccessTotal()
	if err == nil {
		err = s.db.ReachMilestones(total, day)
	}
	if err != nil {
		slog.Warn("statistiche", "err", err)
	}
}

func (s *Server) pill() pillView {
	if !s.cfg.StatsEnabled {
		return pillView{}
	}
	now := s.now()
	day := s.dayOf(now)
	s.stats.mu.Lock()
	defer s.stats.mu.Unlock()
	if s.stats.pillDay == day && now.Sub(s.stats.pillAt) < pillTTL {
		return s.stats.pill
	}
	var p pillView
	if th, ok, err := s.db.MilestoneOn(day); err == nil && ok {
		p.Milestone = th
	}
	if n, err := s.db.UsersOnDay(day); err == nil && n >= 2 {
		p.Count = n
	}
	s.stats.pill, s.stats.pillDay, s.stats.pillAt = p, day, now
	return p
}

// thousands: 10000 → "10.000".
func thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}
