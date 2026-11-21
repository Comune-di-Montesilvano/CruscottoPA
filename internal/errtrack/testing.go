package errtrack

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

// recorder è un sentry.Transport che tiene gli eventi in memoria (solo test).
type recorder struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (r *recorder) Configure(sentry.ClientOptions)        {}
func (r *recorder) Flush(time.Duration) bool              { return true }
func (r *recorder) FlushWithContext(context.Context) bool { return true }
func (r *recorder) Close()                                {}
func (r *recorder) SendEvent(e *sentry.Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

func (r *recorder) list() []*sentry.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*sentry.Event(nil), r.events...)
}

// NewForTest restituisce un tracker che non esce dal processo e la funzione
// che elenca gli eventi "inviati". Per i test di questo e di altri package.
func NewForTest(tb testing.TB) (*Tracker, func() []*sentry.Event) {
	tb.Helper()
	rec := &recorder{}
	t, err := New(Options{
		DSN:         "https://chiave@glitchtip.example.it/1",
		Environment: "test",
		Release:     "test",
		Secret:      "segreto-di-prova-lungo-almeno-32-caratteri",
		Transport:   rec,
	})
	if err != nil {
		tb.Fatal(err)
	}
	return t, rec.list
}
