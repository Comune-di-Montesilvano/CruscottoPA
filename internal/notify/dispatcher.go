package notify

import (
	"context"
	"log/slog"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
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
	Store   Store
	Hub     *Hub
	Pusher  Pusher
	Visible func(username string, alertID int64) bool // chi può vedere l'avviso
	Now     func() time.Time
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

func (d *Dispatcher) RunOnce(ctx context.Context) error {
	now := d.Now()
	alerts, err := d.Store.PendingNotifications(now)
	if err != nil {
		return err
	}
	for _, a := range alerts {
		// Marcato prima dell'invio: al massimo una notifica, anche se il
		// processo si ferma a metà (qualche invio può andare perso: accettato).
		ok, err := d.Store.MarkNotified(a.ID, now)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		visible := func(u string) bool { return d.Visible(u, a.ID) }
		d.Hub.Broadcast(Event{ID: a.ID, Title: a.Title, Level: a.Level}, visible)
		if d.Pusher != nil {
			d.push(ctx, a, visible, now)
		}
		slog.Info("notifica inviata", "alert", a.ID)
	}
	return nil
}

func (d *Dispatcher) push(ctx context.Context, a database.Alert, visible func(string) bool, now time.Time) {
	subs, err := d.Store.ListPushSubscriptions()
	if err != nil {
		slog.Warn("iscrizioni push", "err", err)
		return
	}
	payload := Payload(a)
	for _, s := range subs {
		if !visible(s.Username) {
			continue
		}
		sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		gone, err := d.Pusher.Send(sctx, s, payload, a.Level == database.LevelUrgent)
		cancel()
		switch {
		case gone:
			if err := d.Store.DeletePushSubscription(s.Endpoint); err != nil {
				slog.Warn("rimozione iscrizione push", "err", err)
			}
		case err != nil:
			slog.Warn("invio push", "err", err)
		default:
			d.Store.TouchPushSubscription(s.Endpoint, now)
		}
	}
}
