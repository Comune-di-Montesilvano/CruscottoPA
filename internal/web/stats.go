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
	pillTTL    = time.Minute
	readPctTTL = 5 * time.Minute
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
	// Aperture per i preferiti: due a meno di openGap contano una.
	openGap   = 30 * time.Second
	openAtMax = 10000
)

// statsCache: numeri di gruppo letti dal DB al massimo una volta per TTL.
// Il lock non si tiene durante le query: due richieste possono ricalcolare lo
// stesso valore, nessuna aspetta l'altra.
type statsCache struct {
	mu        sync.Mutex
	pill      pillView
	pillDay   string
	pillAt    time.Time
	pillGen   int           // cresce a ogni nuovo accesso: un calcolo partito prima non si salva
	reads     map[int64]int // letture per avviso
	active    int           // utenti attivi (ActiveWindow)
	readsAt   time.Time
	clicks    map[int64]int // clic per app negli ultimi hotDays giorni
	clicksAt  time.Time
	clickDay  string
	clickSeen map[string]bool      // username|app del giorno clickDay: già contati
	openAt    map[string]time.Time // username|app: ultima apertura contata per i preferiti
}

// reset svuota la cache (test).
func (c *statsCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pill, c.pillDay, c.pillAt = pillView{}, "", time.Time{}
	c.reads, c.active, c.readsAt = nil, 0, time.Time{}
	c.clicks, c.clicksAt, c.clickDay, c.clickSeen, c.openAt = nil, time.Time{}, "", nil, nil
}

// pillView: «Oggi siamo in N» nella testata; Count 0 = nascosta. Con un
// traguardo raggiunto oggi vale quello.
type pillView struct {
	Count     int
	Milestone int64
}

// dayOf: giorno di calendario dell'ente (YYYY-MM-DD).
func (s *Server) dayOf(t time.Time) string { return t.In(s.loc()).Format(database.DayLayout) }

// recordAccess segna l'accesso del giorno e, se è il primo dell'utente oggi,
// controlla i traguardi collettivi (così il traguardo cade nel giorno giusto).
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
	total, err := s.db.AccessTotal()
	if err == nil {
		err = s.db.ReachMilestones(total, day)
	}
	if err != nil {
		slog.Warn("statistiche", "err", err)
	}
	s.stats.mu.Lock()
	s.stats.pillAt, s.stats.pillGen = time.Time{}, s.stats.pillGen+1 // un collega in più: si ricalcola
	s.stats.mu.Unlock()
}

func (s *Server) pill() pillView {
	if !s.cfg.StatsEnabled {
		return pillView{}
	}
	now := s.now()
	day := s.dayOf(now)
	s.stats.mu.Lock()
	if s.stats.pillDay == day && now.Sub(s.stats.pillAt) < pillTTL {
		defer s.stats.mu.Unlock()
		return s.stats.pill
	}
	gen := s.stats.pillGen
	s.stats.mu.Unlock()
	var p pillView
	if th, ok, err := s.db.MilestoneOn(day); err == nil && ok {
		p.Milestone = th
	}
	if n, err := s.db.UsersOnDay(day); err == nil && n >= 2 {
		p.Count = n
	}
	s.stats.mu.Lock()
	if s.stats.pillGen == gen {
		s.stats.pill, s.stats.pillDay, s.stats.pillAt = p, day, now
	}
	s.stats.mu.Unlock()
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
	reads, active := s.stats.reads, s.stats.active
	fresh := reads != nil && now.Sub(s.stats.readsAt) < readPctTTL
	s.stats.mu.Unlock()
	if !fresh {
		r, err := s.db.ReadCounts()
		a, err2 := s.db.ActiveUsernames(now)
		if err != nil || err2 != nil {
			slog.Warn("statistiche letture", "err", errors.Join(err, err2))
			return alerts
		}
		reads, active = r, len(a)
		s.stats.mu.Lock()
		s.stats.reads, s.stats.active, s.stats.readsAt = reads, active, now
		s.stats.mu.Unlock()
	}
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

// handleAppClick conta l'apertura di un'app dalla plancia (tile o ricerca):
// per i preferiti dell'utente (aperture a meno di openGap contano una) e, con
// STATS_ENABLED, un clic aggregato per utente, app e giorno salvato senza
// l'utente. Sempre 200.
func (s *Server) handleAppClick(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	user, _ := s.viewerName(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if user == "" || err != nil {
		return
	}
	now := s.now()
	day := s.dayOf(now)
	key := user + "|" + strconv.FormatInt(id, 10)
	s.stats.mu.Lock()
	clickDue := s.cfg.StatsEnabled && !(s.stats.clickDay == day && s.stats.clickSeen[key])
	last, ok := s.stats.openAt[key]
	openDue := !ok || now.Sub(last) >= openGap
	s.stats.mu.Unlock()
	// La visibilità costa (dashboard + gruppi): solo se c'è qualcosa da contare.
	if (!clickDue && !openDue) || !s.appVisibleTo(r, id) {
		return
	}
	s.stats.mu.Lock()
	if openDue {
		last, ok = s.stats.openAt[key]
		openDue = !ok || now.Sub(last) >= openGap
		if openDue {
			if s.stats.openAt == nil || len(s.stats.openAt) > openAtMax {
				s.stats.openAt = pruneOpens(s.stats.openAt, now)
			}
			s.stats.openAt[key] = now
		}
	}
	if clickDue {
		if s.stats.clickDay != day {
			s.stats.clickDay, s.stats.clickSeen = day, map[string]bool{}
		}
		clickDue = !s.stats.clickSeen[key]
		s.stats.clickSeen[key] = true
	}
	s.stats.mu.Unlock()
	if openDue {
		if err := s.db.AddAppOpen(user, id); err != nil {
			slog.Warn("aperture", "err", err)
		}
	}
	if clickDue {
		if err := s.db.AddAppClick(id, day); err != nil {
			slog.Warn("statistiche clic", "err", err)
		}
	}
}

// pruneOpens tiene solo le aperture ancora dentro openGap.
func pruneOpens(m map[string]time.Time, now time.Time) map[string]time.Time {
	out := map[string]time.Time{}
	for k, t := range m {
		if now.Sub(t) < openGap {
			out[k] = t
		}
	}
	return out
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
	clicks := s.stats.clicks
	fresh := clicks != nil && now.Sub(s.stats.clicksAt) < hotTTL
	s.stats.mu.Unlock()
	if !fresh {
		m, err := s.db.AppClicksSince(s.dayOf(now.AddDate(0, 0, -(hotDays - 1))))
		if err != nil {
			slog.Warn("statistiche clic", "err", err)
			return
		}
		clicks = m
		s.stats.mu.Lock()
		s.stats.clicks, s.stats.clicksAt = m, now
		s.stats.mu.Unlock()
	}
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
