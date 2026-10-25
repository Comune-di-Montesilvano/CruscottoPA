package web

import (
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// «Noi del Comune»: numeri di gruppo e traguardi personali in plancia. Mai
// classifiche tra persone né orari: dei giorni di accesso si salva solo il
// giorno. Dati indicativi (identità dichiarata), solo per personalizzare.

const (
	pillTTL        = time.Minute
	milestoneEvery = time.Minute
	readPctTTL     = 5 * time.Minute
	// «Letto dal X%»: solo con abbastanza colleghi attivi, avviso in giro da un
	// po' e una percentuale che non mette a disagio.
	readPctMinActive = 10
	readPctMinAge    = time.Hour
	readPctMin       = 10
)

// statsCache: numeri di gruppo letti dal DB al massimo una volta per TTL.
type statsCache struct {
	mu          sync.Mutex
	pill        pillView
	pillDay     string
	pillAt      time.Time
	milestoneAt time.Time
	reads       map[int64]int // letture per avviso
	active      int           // utenti attivi (ActiveWindow)
	readsAt     time.Time
}

// reset svuota la cache (test).
func (c *statsCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pill, c.pillDay, c.pillAt, c.milestoneAt = pillView{}, "", time.Time{}, time.Time{}
	c.reads, c.active, c.readsAt = nil, 0, time.Time{}
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

// withReadPct: «Letto dal X% dei colleghi» sugli avvisi pubblici. Per quelli
// riservati il denominatore giusto richiederebbe AD per ogni utente.
func (s *Server) withReadPct(alerts []database.Alert, f *contentFilter) []database.Alert {
	if !s.cfg.StatsEnabled {
		return alerts
	}
	now := s.now()
	s.stats.mu.Lock()
	if s.stats.reads == nil || now.Sub(s.stats.readsAt) >= readPctTTL {
		reads, err := s.db.ReadCounts()
		active, err2 := s.db.ActiveUsernames(now)
		if err != nil || err2 != nil {
			s.stats.mu.Unlock()
			slog.Warn("statistiche letture", "err", errors.Join(err, err2))
			return alerts
		}
		s.stats.reads, s.stats.active, s.stats.readsAt = reads, len(active), now
	}
	reads, active := s.stats.reads, s.stats.active
	s.stats.mu.Unlock()
	if active < readPctMinActive {
		return alerts
	}
	for i, a := range alerts {
		if f.alerts[a.ID].Mode != audience.ModePublic || now.Sub(a.StartsAt) < readPctMinAge {
			continue
		}
		if pct := min(100, (reads[a.ID]*100+active/2)/active); pct >= readPctMin {
			alerts[i].ReadPct = pct
		}
	}
	return alerts
}

// thousands: 10000 → "10.000".
func thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}
