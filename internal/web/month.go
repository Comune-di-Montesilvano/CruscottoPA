package web

import (
	"fmt"
	"math"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// Widget «Il tuo mese»: numeri solo di chi guarda (mai confronti con altri),
// spille e la riga di gruppo dell'assistenza. Solo per gli utenti riconosciuti.

// badgeView: spilla; Earned = presa, altrimenti Hint dice come ottenerla.
type badgeView struct {
	Key, Icon, Label, Hint string
	Earned                 bool
}

type monthView struct {
	MonthName      string
	Days           int
	Tickets        bool // modulo ticket attivo
	Sent, Resolved int
	Badges         []badgeView
	Assist         *database.TicketStats
}

// monthFor: nil per gli anonimi o con le statistiche spente.
func (s *Server) monthFor(username string, f *contentFilter) (*monthView, error) {
	if username == "" || !s.cfg.StatsEnabled {
		return nil, nil
	}
	now := s.now()
	from := s.monthStart(now)
	to := from.AddDate(0, 1, 0)
	month := from.Format("2006-01")
	v := &monthView{MonthName: months[from.Month()-1], Tickets: s.ticketsEnabled()}
	var err error
	if v.Days, err = s.db.UserDaysBetween(username, s.dayOf(from), s.dayOf(to.Add(-time.Second))); err != nil {
		return nil, err
	}

	push, err := s.db.HasPushSubscription(username)
	if err != nil {
		return nil, err
	}
	p, _, err := s.db.GetPresence(username)
	if err != nil {
		return nil, err
	}
	v.Badges = append(v.Badges,
		badgeView{Key: "notifiche", Icon: "notifications_active", Label: "Notifiche attive", Earned: push || p.Permission == "granted",
			Hint: "Attiva le notifiche per ricevere gli avvisi urgenti"},
		badgeView{Key: "app", Icon: "install_desktop", Label: "App installata", Earned: p.LastApp != nil,
			Hint: "Installa il cruscotto come app dal menu del browser (⋯ → App → Installa)"})

	// Avvisi del mese ancora attivi e destinati all'utente: chi era in ferie
	// quando uno è scaduto non perde la spilla per sempre.
	alerts, err := s.db.ListActiveAlerts(now)
	if err != nil {
		return nil, err
	}
	read, err := s.db.ReadAlertIDs(username)
	if err != nil {
		return nil, err
	}
	count, allRead := 0, true
	for _, a := range alerts {
		if a.StartsAt.Before(from) || !f.visible(f.alerts[a.ID]) {
			continue
		}
		count++
		allRead = allRead && read[a.ID]
	}
	if count > 0 {
		v.Badges = append(v.Badges, badgeView{Key: "avvisi", Icon: "task_alt", Label: "Tutti gli avvisi del mese letti", Earned: allRead,
			Hint: "Leggi tutti gli avvisi del mese"})
	}

	if v.Tickets {
		ids, err := s.db.TicketsOpenedBetween(username, from, to)
		if err != nil {
			return nil, err
		}
		v.Sent = len(ids)
		if v.Resolved, err = s.db.ClosedInMonth(month, ids); err != nil {
			return nil, err
		}
		opened, err := s.db.HasOpenedTicket(username)
		if err != nil {
			return nil, err
		}
		v.Badges = append(v.Badges, badgeView{Key: "ticket", Icon: "confirmation_number", Label: "Prima richiesta di assistenza", Earned: opened,
			Hint: "Quando ti serve aiuto, apri una richiesta dal riquadro Assistenza"})
		st, ok, err := s.db.GetTicketStats(month)
		if err != nil {
			return nil, err
		}
		if ok {
			v.Assist = &st
		}
	}
	return v, nil
}

// roughDuration: tempo arrotondato a parole («2 ore», «3 giorni»).
func roughDuration(d time.Duration) string {
	units := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %s", n, many)
	}
	switch {
	case d < time.Hour:
		return units(max(1, int(math.Round(d.Minutes()))), "minuto", "minuti")
	case d <= 48*time.Hour:
		return units(int(math.Round(d.Hours())), "ora", "ore")
	default:
		return units(int(math.Round(d.Hours()/24)), "giorno", "giorni")
	}
}

// plural: «1 richiesta inviata» / «2 richieste inviate».
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
