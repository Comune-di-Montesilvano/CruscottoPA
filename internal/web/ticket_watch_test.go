package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
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
