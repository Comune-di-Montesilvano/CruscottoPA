package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/notify"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

type fakePusher struct{ sent []string }

func (f *fakePusher) Send(_ context.Context, sub database.PushSubscription, payload []byte, _ bool) (bool, error) {
	f.sent = append(f.sent, sub.Username+" "+string(payload))
	return false, nil
}

func watchServer(t *testing.T) (*Server, *otrs.Mock, *fakePusher, *notify.Client) {
	t.Helper()
	m := mockConversation()
	s, _ := ticketTestServer(t, m)
	fp := &fakePusher{}
	s.pusher = fp
	s.db.UpsertTicketUser("mrossi@example.it", "mrossi", fixedNow)
	if _, err := s.db.SavePushSubscription(database.PushSubscription{Endpoint: "https://fcm.googleapis.com/x", P256dh: "k", Auth: "a", Username: "mrossi", CreatedAt: fixedNow}, 100); err != nil {
		t.Fatal(err)
	}
	c, err := s.hub.Subscribe("mrossi")
	if err != nil {
		t.Fatal(err)
	}
	return s, m, fp, c
}

func TestTicketWatchFirstRunSilent(t *testing.T) {
	s, _, fp, c := watchServer(t)
	w := &ticketWatcher{}
	if err := s.ticketWatchOnce(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if len(fp.sent) != 0 || len(c.Events) != 0 {
		t.Fatalf("primo giro: notifiche inviate %v", fp.sent)
	}
	if last, ok, _ := s.db.TicketNotifyState("5"); !ok || last != 102 {
		t.Fatalf("stato dopo il primo giro: %d %v", last, ok)
	}
}

func TestTicketWatchNotifiesAgentReplyOnce(t *testing.T) {
	s, m, fp, c := watchServer(t)
	w := &ticketWatcher{}
	s.ticketWatchOnce(context.Background(), w)
	tk := m.Tickets["5"]
	tk.Changed = fixedNow.Add(time.Minute)
	tk.Articles = append(tk.Articles,
		otrs.Article{ArticleID: "110", FromAgent: true, Body: "Fatto: ora funziona.", Created: fixedNow.Add(time.Minute)},
		otrs.Article{ArticleID: "111", FromAgent: false, Body: "Grazie", Created: fixedNow.Add(time.Minute)})
	s.ticketWatchOnce(context.Background(), w)
	if len(fp.sent) != 1 {
		t.Fatalf("push: %v", fp.sent)
	}
	select {
	case e := <-c.Events:
		if e.Kind != "ticket" || e.URL != "/ticket/5" || e.Body != "Fatto: ora funziona." {
			t.Fatalf("evento: %+v", e)
		}
	default:
		t.Fatal("nessun evento SSE")
	}
	s.ticketWatchOnce(context.Background(), w) // stesso articolo: niente di nuovo
	if len(fp.sent) != 1 {
		t.Fatalf("notifica ripetuta: %v", fp.sent)
	}
}

func TestTicketWatchCustomerReplyOrUnknownUser(t *testing.T) {
	s, m, fp, _ := watchServer(t)
	w := &ticketWatcher{}
	s.ticketWatchOnce(context.Background(), w)
	m.Tickets["5"].Changed = fixedNow.Add(time.Minute)
	m.Tickets["5"].Articles = append(m.Tickets["5"].Articles, otrs.Article{ArticleID: "120", Body: "mia risposta"})
	m.Tickets["9"].Changed = fixedNow.Add(time.Minute) // cliente senza utente in plancia
	m.Tickets["9"].Articles = []otrs.Article{{ArticleID: "130", FromAgent: true, Body: "x"}}
	s.ticketWatchOnce(context.Background(), w)
	if len(fp.sent) != 0 {
		t.Fatalf("notifiche non dovute: %v", fp.sent)
	}
}

func TestTicketWatchOTRSDownKeepsSince(t *testing.T) {
	s, m, _, _ := watchServer(t)
	w := &ticketWatcher{}
	s.ticketWatchOnce(context.Background(), w)
	before := w.since
	m.Err = otrs.ErrOTRS
	if err := s.ticketWatchOnce(context.Background(), w); err == nil || !w.since.Equal(before) {
		t.Fatalf("OTRS giù: %v %v→%v", err, before, w.since)
	}
}

func TestTicketEventsScript(t *testing.T) {
	js := readFile(t, "../../web/static/js/notifiche.js")
	for _, want := range []string{`addEventListener("ticket"`, `kind === "ticket"`, `"ticket-" + t.id`} {
		if !strings.Contains(js, want) {
			t.Errorf("notifiche.js: manca %q", want)
		}
	}
}

// Ticket senza stato (fuori dal primo giro, o pulito dopo 30 giorni): una
// risposta vecchia dell'operatore non va notificata quando il ticket cambia.
func TestTicketWatchUnstatedTicketOldReply(t *testing.T) {
	s, m, fp, _ := watchServer(t)
	w := &ticketWatcher{}
	s.ticketWatchOnce(context.Background(), w)
	m.Tickets["10"] = &otrs.Ticket{Summary: otrs.Summary{TicketID: "10", TicketNumber: "N10", Changed: fixedNow.Add(time.Minute)},
		CustomerUserID: "mrossi@example.it", Articles: []otrs.Article{
			{ArticleID: "300", Body: "domanda", Created: fixedNow.AddDate(0, 0, -10)},
			{ArticleID: "301", FromAgent: true, Body: "risposta vecchia", Created: fixedNow.AddDate(0, 0, -9)},
		}}
	s.ticketWatchOnce(context.Background(), w)
	if len(fp.sent) != 0 {
		t.Fatalf("notificata una risposta vecchia: %v", fp.sent)
	}
	m.Tickets["10"].Changed = fixedNow.Add(2 * time.Minute)
	m.Tickets["10"].Articles = append(m.Tickets["10"].Articles, otrs.Article{ArticleID: "302", FromAgent: true, Body: "nuova", Created: fixedNow.Add(2 * time.Minute)})
	s.ticketWatchOnce(context.Background(), w)
	if len(fp.sent) != 1 || !strings.Contains(fp.sent[0], "nuova") {
		t.Fatalf("risposta nuova: %v", fp.sent)
	}
}

// OTRS giù al primo giro: quando torna, nessuna valanga di risposte vecchie.
func TestTicketWatchFirstRunFailureNoFlood(t *testing.T) {
	s, m, fp, _ := watchServer(t)
	m.Err = otrs.ErrOTRS
	w := &ticketWatcher{}
	if err := s.ticketWatchOnce(context.Background(), w); err == nil {
		t.Fatal("atteso errore con OTRS giù")
	}
	m.Err = nil
	s.ticketWatchOnce(context.Background(), w)
	if len(fp.sent) != 0 {
		t.Fatalf("risposte vecchie notificate dopo un primo giro fallito: %v", fp.sent)
	}
}

// Un ticket che non si legge non blocca gli altri.
func TestTicketWatchPartialFailure(t *testing.T) {
	s, m, fp, _ := watchServer(t)
	w := &ticketWatcher{}
	s.ticketWatchOnce(context.Background(), w)
	pc := &partialChanged{Mock: m}
	s.tickets = pc
	m.Tickets["5"].Changed = fixedNow.Add(time.Minute)
	m.Tickets["5"].Articles = append(m.Tickets["5"].Articles, otrs.Article{ArticleID: "140", FromAgent: true, Body: "ok", Created: fixedNow.Add(time.Minute)})
	before := w.since
	err := s.ticketWatchOnce(context.Background(), w)
	if err == nil || !w.since.Equal(before) || len(fp.sent) != 1 {
		t.Fatalf("parziale: err=%v since %v→%v push=%v", err, before, w.since, fp.sent)
	}
}

// partialChanged: Changed restituisce i ticket letti e un errore per gli altri.
type partialChanged struct{ *otrs.Mock }

func (p *partialChanged) Changed(ctx context.Context, since time.Time) ([]otrs.Ticket, error) {
	ts, _ := p.Mock.Changed(ctx, since)
	return ts, otrs.ErrOTRS
}

// La cache dura più del refresh del widget; ogni ticket cambiato la svuota.
func TestTicketCacheRefreshAndWatcherForget(t *testing.T) {
	if ticketCacheTTL < 5*time.Minute {
		t.Fatalf("TTL della cache: %v", ticketCacheTTL)
	}
	s, m, _, _ := watchServer(t)
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	if page := do(t, s, "GET", "/", nil, c, nil).Body.String(); !strings.Contains(page, "every 300s") {
		t.Error("refresh del widget: atteso ogni 300 s")
	}
	w := &ticketWatcher{}
	s.ticketWatchOnce(context.Background(), w)
	do(t, s, "GET", "/partials/ticket", nil, c, nil) // in cache
	m.Tickets["5"].Title = "Titolo cambiato"
	m.Tickets["5"].Changed = fixedNow.Add(time.Minute)
	s.ticketWatchOnce(context.Background(), w)
	if body := do(t, s, "GET", "/partials/ticket", nil, c, nil).Body.String(); !strings.Contains(body, "Titolo cambiato") {
		t.Fatal("il watcher non ha svuotato la cache del ticket cambiato")
	}
}
