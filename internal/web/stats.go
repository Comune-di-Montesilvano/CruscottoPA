package web

import (
	"errors"
	"log/slog"
	"net/http"
	"sort"
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
	// Fiammella: le prime hotCount app degli ultimi hotDays giorni con almeno
	// hotMinClicks clic (un clic per utente, app e giorno).
	hotTTL       = 10 * time.Minute
	hotDays      = 7
	hotCount     = 3
	hotMinClicks = 5
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
	clicks      map[int64]int // clic per app negli ultimi hotDays giorni
	clicksAt    time.Time
	clickDay    string
	clickSeen   map[string]bool // username|app del giorno clickDay: già contati
}

// reset svuota la cache (test).
func (c *statsCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pill, c.pillDay, c.pillAt, c.milestoneAt = pillView{}, "", time.Time{}, time.Time{}
	c.reads, c.active, c.readsAt = nil, 0, time.Time{}
	c.clicks, c.clicksAt, c.clickDay, c.clickSeen = nil, time.Time{}, "", nil
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

// handleAppClick conta l'apertura di un'app dalla plancia: un clic per
// utente, app e giorno, salvato senza l'utente. Sempre 200.
func (s *Server) handleAppClick(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	user, _ := s.viewerName(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if !s.cfg.StatsEnabled || user == "" || err != nil || !s.appVisibleTo(r, id) {
		return
	}
	day := s.dayOf(s.now())
	key := user + "|" + strconv.FormatInt(id, 10)
	s.stats.mu.Lock()
	if s.stats.clickDay != day {
		s.stats.clickDay, s.stats.clickSeen = day, map[string]bool{}
	}
	seen := s.stats.clickSeen[key]
	s.stats.clickSeen[key] = true
	s.stats.mu.Unlock()
	if seen {
		return
	}
	if err := s.db.AddAppClick(id, day); err != nil {
		slog.Warn("statistiche clic", "err", err)
	}
}

// appVisibleTo: app in plancia (attiva, con indirizzo) e destinata a chi guarda
// (senza «Mostra tutto»).
func (s *Server) appVisibleTo(r *http.Request, id int64) bool {
	d, err := s.db.GetDashboard(s.now())
	if err != nil {
		return false
	}
	f, err := s.contentFilterFor(r)
	if err != nil {
		return false
	}
	for _, c := range d.Categories {
		for _, a := range c.Apps {
			if a.ID == id {
				return f.visible(f.apps[id])
			}
		}
	}
	return false
}

// markHot accende la fiammella sulle app più aperte tra quelle in pagina.
func (s *Server) markHot(d *database.Dashboard) {
	if !s.cfg.StatsEnabled {
		return
	}
	now := s.now()
	s.stats.mu.Lock()
	if s.stats.clicks == nil || now.Sub(s.stats.clicksAt) >= hotTTL {
		m, err := s.db.AppClicksSince(s.dayOf(now.AddDate(0, 0, -(hotDays - 1))))
		if err != nil {
			s.stats.mu.Unlock()
			slog.Warn("statistiche clic", "err", err)
			return
		}
		s.stats.clicks, s.stats.clicksAt = m, now
	}
	clicks := s.stats.clicks
	s.stats.mu.Unlock()
	var ids []int64
	for _, c := range d.Categories {
		for _, a := range c.Apps {
			if clicks[a.ID] >= hotMinClicks {
				ids = append(ids, a.ID)
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if clicks[ids[i]] != clicks[ids[j]] {
			return clicks[ids[i]] > clicks[ids[j]]
		}
		return ids[i] < ids[j]
	})
	hot := map[int64]bool{}
	for _, id := range ids[:min(len(ids), hotCount)] {
		hot[id] = true
	}
	for ci := range d.Categories {
		for ai := range d.Categories[ci].Apps {
			d.Categories[ci].Apps[ai].Hot = hot[d.Categories[ci].Apps[ai].ID]
		}
	}
}

// thousands: 10000 → "10.000".
func thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}
