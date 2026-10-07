package notify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestHubBroadcastFiltered(t *testing.T) {
	h := NewHub(10)
	a, _ := h.Subscribe("mrossi")
	b, _ := h.Subscribe("")
	h.Broadcast(Event{ID: 1, Title: "riservato"}, func(u string) bool { return u == "mrossi" })
	select {
	case e := <-a.Events:
		if e.ID != 1 {
			t.Fatalf("evento: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("l'utente destinatario non ha ricevuto l'evento")
	}
	select {
	case e := <-b.Events:
		t.Fatalf("l'anonimo non doveva ricevere %+v", e)
	default:
	}
	h.Unsubscribe(a)
	h.Unsubscribe(b)
}

func TestHubLimitAndClose(t *testing.T) {
	h := NewHub(1)
	if _, err := h.Subscribe("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Subscribe("b"); !errors.Is(err, ErrTooMany) {
		t.Fatalf("oltre il limite: %v", err)
	}
	h.Close()
	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("Close deve chiudere Done()")
	}
	if _, err := h.Subscribe("c"); err == nil {
		t.Fatal("dopo Close nessuna nuova connessione")
	}
}

type fakeStore struct {
	mu      sync.Mutex
	pending []database.Alert
	marked  map[int64]bool
	subs    []database.PushSubscription
	deleted []string
	touched []string
}

func (f *fakeStore) PendingNotifications(time.Time) ([]database.Alert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []database.Alert{}
	for _, a := range f.pending {
		if !f.marked[a.ID] {
			out = append(out, a)
		}
	}
	return out, nil
}
func (f *fakeStore) MarkNotified(id int64, _ time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.marked[id] {
		return false, nil
	}
	f.marked[id] = true
	return true, nil
}
func (f *fakeStore) ListPushSubscriptions() ([]database.PushSubscription, error) { return f.subs, nil }
func (f *fakeStore) DeletePushSubscription(e string) error {
	f.deleted = append(f.deleted, e)
	return nil
}
func (f *fakeStore) TouchPushSubscription(e string, _ time.Time) error {
	f.touched = append(f.touched, e)
	return nil
}

type fakePusher struct {
	sent []string
	gone map[string]bool
}

func (p *fakePusher) Send(_ context.Context, s database.PushSubscription, _ []byte, _ bool) (bool, error) {
	p.sent = append(p.sent, s.Endpoint)
	return p.gone[s.Endpoint], nil
}

func TestDispatcherNotifiesOnceAndOnlyVisible(t *testing.T) {
	st := &fakeStore{
		pending: []database.Alert{{ID: 1, Title: "urgente", Level: database.LevelUrgent, Notify: true}},
		marked:  map[int64]bool{},
		subs: []database.PushSubscription{
			{Endpoint: "https://p/mrossi", Username: "mrossi"},
			{Endpoint: "https://p/anon"},
			{Endpoint: "https://p/vecchio", Username: "mrossi"},
		},
	}
	pu := &fakePusher{gone: map[string]bool{"https://p/vecchio": true}}
	hub := NewHub(10)
	c, _ := hub.Subscribe("mrossi")
	d := &Dispatcher{Store: st, Hub: hub, Pusher: pu, Now: time.Now,
		Visible: func(u string, id int64) bool { return u == "mrossi" }}

	if err := d.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pu.sent) != 2 || pu.sent[0] != "https://p/mrossi" {
		t.Fatalf("push solo ai destinatari: %v", pu.sent)
	}
	if len(st.deleted) != 1 || st.deleted[0] != "https://p/vecchio" {
		t.Fatalf("iscrizione scaduta da cancellare: %v", st.deleted)
	}
	select {
	case <-c.Events:
	case <-time.After(time.Second):
		t.Fatal("evento SSE mancante")
	}
	if err := d.RunOnce(context.Background()); err != nil || len(pu.sent) != 2 {
		t.Fatalf("secondo ciclo: nessuna nuova notifica, inviati %v", pu.sent)
	}
}

func TestDispatcherWithoutPush(t *testing.T) {
	st := &fakeStore{pending: []database.Alert{{ID: 2, Title: "x", Notify: true}}, marked: map[int64]bool{}}
	d := &Dispatcher{Store: st, Hub: NewHub(1), Now: time.Now, Visible: func(string, int64) bool { return true }}
	if err := d.RunOnce(context.Background()); err != nil || !st.marked[2] {
		t.Fatalf("senza Pusher: avviso comunque marcato, err=%v", err)
	}
}
