package notify

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	records map[string]string // endpoint → stato della consegna
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
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, e)
	return nil
}
func (f *fakeStore) TouchPushSubscription(e string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touched = append(f.touched, e)
	return nil
}

func (f *fakeStore) RecordDelivery(_ int64, _, endpoint, _, status string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.records == nil {
		f.records = map[string]string{}
	}
	f.records[endpoint] = status
	return nil
}

type fakePusher struct {
	mu   sync.Mutex
	sent []string
	gone map[string]bool
	wait chan struct{} // se non nil, ogni invio aspetta che venga chiuso
}

func (p *fakePusher) Send(_ context.Context, s database.PushSubscription, _ []byte, _ bool) (bool, error) {
	if p.wait != nil {
		<-p.wait
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, s.Endpoint)
	return p.gone[s.Endpoint], nil
}

func visibleTo(users ...string) func(string, int64) (bool, bool) {
	return func(u string, _ int64) (bool, bool) {
		for _, x := range users {
			if u == x {
				return true, false
			}
		}
		return false, false
	}
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
		Visible: visibleTo("mrossi")}

	if err := d.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	sort.Strings(pu.sent)
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
	d := &Dispatcher{Store: st, Hub: NewHub(1), Now: time.Now, Visible: func(string, int64) (bool, bool) { return true, false }}
	if err := d.RunOnce(context.Background()); err != nil || !st.marked[2] {
		t.Fatalf("senza Pusher: avviso comunque marcato, err=%v", err)
	}
}

// Un servizio push lento non deve ritardare le plance aperte (SSE) né gli
// altri invii: le notifiche push partono in parallelo, dopo gli eventi SSE.
func TestDispatcherSSEBeforeSlowPush(t *testing.T) {
	subs := []database.PushSubscription{}
	for i := 0; i < 3; i++ {
		subs = append(subs, database.PushSubscription{Endpoint: fmt.Sprintf("https://p/%d", i), Username: "mrossi"})
	}
	st := &fakeStore{marked: map[int64]bool{}, subs: subs, pending: []database.Alert{
		{ID: 1, Title: "uno", Notify: true}, {ID: 2, Title: "due", Notify: true}}}
	release := make(chan struct{})
	pu := &fakePusher{wait: release}
	hub := NewHub(10)
	c, _ := hub.Subscribe("mrossi")
	d := &Dispatcher{Store: st, Hub: hub, Pusher: pu, Now: time.Now, Visible: visibleTo("mrossi")}
	done := make(chan error)
	go func() { done <- d.RunOnce(context.Background()) }()
	for i := 0; i < 2; i++ {
		select {
		case <-c.Events:
		case <-time.After(time.Second):
			t.Fatalf("evento SSE %d bloccato dietro l'invio push", i+1)
		}
	}
	close(release)
	if err := <-done; err != nil || len(pu.sent) != 6 {
		t.Fatalf("invii: %v %v", pu.sent, err)
	}
}

// AD non raggiungibile: un avviso riservato non va "consumato" inviandolo a
// nessuno; si riprova ai cicli successivi, al massimo per 10 minuti.
func TestDispatcherDefersWhenAudienceUnknown(t *testing.T) {
	start := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	now := start.Add(time.Minute)
	st := &fakeStore{marked: map[int64]bool{}, pending: []database.Alert{{ID: 3, Title: "stipendi", Notify: true, StartsAt: start}}}
	hub := NewHub(10)
	c, _ := hub.Subscribe("mrossi")
	adDown := true
	d := &Dispatcher{Store: st, Hub: hub, Now: func() time.Time { return now },
		Visible: func(u string, _ int64) (bool, bool) { return !adDown && u == "mrossi", adDown && u != "" }}
	d.RunOnce(context.Background())
	if st.marked[3] {
		t.Fatal("AD giù: l'avviso non doveva essere marcato")
	}
	adDown = false
	d.RunOnce(context.Background())
	if !st.marked[3] {
		t.Fatal("AD tornato: l'avviso andava notificato")
	}
	select {
	case <-c.Events:
	case <-time.After(time.Second):
		t.Fatal("evento mancante dopo il ritorno di AD")
	}

	st2 := &fakeStore{marked: map[int64]bool{}, pending: []database.Alert{{ID: 4, Title: "x", Notify: true, StartsAt: start}}}
	now = start.Add(11 * time.Minute)
	d2 := &Dispatcher{Store: st2, Hub: hub, Now: func() time.Time { return now },
		Visible: func(u string, _ int64) (bool, bool) { return false, u != "" }}
	d2.RunOnce(context.Background())
	if !st2.marked[4] {
		t.Fatal("oltre 10 minuti non si rimanda più")
	}
}

// Il dispatcher registra l'esito di ogni invio (per la pagina Letture).
func TestDispatcherRecordsDeliveries(t *testing.T) {
	st := &fakeStore{
		pending: []database.Alert{{ID: 1, Title: "x", Level: database.LevelNews, Notify: true}},
		marked:  map[int64]bool{},
		subs: []database.PushSubscription{
			{Endpoint: "https://p/mrossi", Username: "mrossi"},
			{Endpoint: "https://p/vecchio", Username: "mrossi"},
		},
	}
	pu := &fakePusher{gone: map[string]bool{"https://p/vecchio": true}}
	d := &Dispatcher{Store: st, Hub: NewHub(1), Pusher: pu, Now: time.Now, Visible: visibleTo("mrossi")}
	if err := d.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st.records["https://p/mrossi"] != database.DeliverySent || st.records["https://p/vecchio"] != database.DeliveryGone {
		t.Fatalf("consegne registrate: %v", st.records)
	}
}

func TestServiceName(t *testing.T) {
	for in, want := range map[string]string{
		"https://fcm.googleapis.com/fcm/send/x":           "Chrome/Edge",
		"https://wns2-par02p.notify.windows.com/w/?t=1":   "Edge (Windows)",
		"https://updates.push.services.mozilla.com/wpush": "Firefox",
		"https://web.push.apple.com/abc":                  "Safari",
		"https://altro.example/x":                         "altro",
	} {
		if got := ServiceName(in); got != want {
			t.Errorf("ServiceName(%q) = %q, atteso %q", in, got, want)
		}
	}
}
