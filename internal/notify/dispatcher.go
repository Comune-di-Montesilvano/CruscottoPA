package notify

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

const (
	// deferLimit: per quanto si rimanda un avviso riservato se AD non risponde.
	deferLimit = 10 * time.Minute
	// pushWorkers e pushTimeout: invii in parallelo, ciascuno con un limite,
	// così un servizio lento non ferma gli altri.
	pushWorkers = 8
	pushTimeout = 5 * time.Second
)

// Store: ciò che il dispatcher legge e scrive nel database.
type Store interface {
	PendingNotifications(now time.Time) ([]database.Alert, error)
	MarkNotified(id int64, at time.Time) (bool, error)
	ListPushSubscriptions() ([]database.PushSubscription, error)
	DeletePushSubscription(endpoint string) error
	TouchPushSubscription(endpoint string, at time.Time) error
}

// Dispatcher manda una sola volta la notifica degli avvisi che la richiedono,
// alle plance aperte (Hub) e ai browser iscritti (Pusher, nil = push spento).
type Dispatcher struct {
	Store  Store
	Hub    *Hub
	Pusher Pusher
	// Visible: chi può vedere l'avviso. unsure = non si può stabilire adesso
	// (AD non disponibile): l'avviso si rimanda.
	Visible func(username string, alertID int64) (visible, unsure bool)
	Now     func() time.Time
}

type pushJob struct {
	alert database.Alert
	sub   database.PushSubscription
}

func (d *Dispatcher) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := d.RunOnce(ctx); err != nil {
			slog.Warn("notifiche", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RunOnce: prima gli eventi SSE di tutti gli avvisi, poi gli invii push in
// parallelo.
func (d *Dispatcher) RunOnce(ctx context.Context) error {
	now := d.Now()
	alerts, err := d.Store.PendingNotifications(now)
	if err != nil || len(alerts) == 0 {
		return err
	}
	var subs []database.PushSubscription
	if d.Pusher != nil {
		if subs, err = d.Store.ListPushSubscriptions(); err != nil {
			slog.Warn("iscrizioni push", "err", err)
		}
	}
	var jobs []pushJob
	for _, a := range alerts {
		// Visibilità calcolata una volta per utente, prima di marcare: con AD
		// giù un avviso riservato non va consumato senza raggiungere nessuno.
		seen := map[string]bool{}
		unsure := false
		check := func(u string) {
			if _, ok := seen[u]; ok {
				return
			}
			v, un := d.Visible(u, a.ID)
			seen[u] = v
			unsure = unsure || un
		}
		for _, u := range d.Hub.Usernames() {
			check(u)
		}
		for _, s := range subs {
			check(s.Username)
		}
		if unsure {
			if now.Sub(a.StartsAt) < deferLimit {
				slog.Info("notifica rimandata: destinatari non verificabili (AD non disponibile)", "alert", a.ID)
				continue
			}
			slog.Warn("AD non disponibile: notifica inviata solo ai destinatari verificati", "alert", a.ID)
		}
		// Marcato prima dell'invio: al massimo una notifica, anche se il
		// processo si ferma a metà (qualche invio può andare perso: accettato).
		ok, err := d.Store.MarkNotified(a.ID, now)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		d.Hub.Broadcast(Event{ID: a.ID, Title: a.Title, Level: a.Level}, func(u string) bool {
			if v, ok := seen[u]; ok {
				return v
			}
			v, _ := d.Visible(u, a.ID) // plancia aperta nel frattempo
			return v
		})
		for _, s := range subs {
			if seen[s.Username] {
				jobs = append(jobs, pushJob{alert: a, sub: s})
			}
		}
		slog.Info("notifica inviata", "alert", a.ID)
	}
	d.pushAll(ctx, jobs, now)
	return nil
}

func (d *Dispatcher) pushAll(ctx context.Context, jobs []pushJob, now time.Time) {
	ch := make(chan pushJob)
	var wg sync.WaitGroup
	for i := 0; i < pushWorkers && i < len(jobs); i++ {
		wg.Go(func() {
			for j := range ch {
				d.push(ctx, j, now)
			}
		})
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
}

func (d *Dispatcher) push(ctx context.Context, j pushJob, now time.Time) {
	sctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	gone, err := d.Pusher.Send(sctx, j.sub, Payload(j.alert), j.alert.Level == database.LevelUrgent)
	switch {
	case gone:
		if err := d.Store.DeletePushSubscription(j.sub.Endpoint); err != nil {
			slog.Warn("rimozione iscrizione push", "err", err)
		}
	case err != nil:
		slog.Warn("invio push", "err", err)
	default:
		d.Store.TouchPushSubscription(j.sub.Endpoint, now)
	}
}
