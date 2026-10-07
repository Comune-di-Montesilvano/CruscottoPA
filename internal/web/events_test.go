package web

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/notify"
)

func TestEventsStream(t *testing.T) {
	sseHeartbeat = 50 * time.Millisecond
	defer func() { sseHeartbeat = 25 * time.Second }()
	s, _ := newTestServer(t, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/eventi", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("header: %v", resp.Header)
	}
	r := bufio.NewReader(resp.Body)
	if line, _ := r.ReadString('\n'); !strings.HasPrefix(line, ":") {
		t.Fatalf("primo heartbeat atteso, ottenuto %q", line)
	}
	s.hub.Broadcast(notify.Event{ID: 9, Title: "Sciopero", Level: "urgent"}, func(string) bool { return true })
	deadline := time.After(2 * time.Second)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, `"Sciopero"`) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("evento non ricevuto")
		default:
		}
	}
}

func TestEventsCloseUnblocks(t *testing.T) {
	s, _ := newTestServer(t, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/eventi")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	done := make(chan struct{})
	go func() { bufio.NewReader(resp.Body).ReadString(0); close(done) }()
	s.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close deve chiudere i flussi SSE (shutdown rapido)")
	}
}

func TestAlertVisibleTo(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	trib, _ := db.CreateAudienceGroup("Tributi")
	db.AddAudienceRule(database.AudienceRule{GroupID: trib, Kind: audience.KindAttr, Attr: "physicalDeliveryOfficeName", Value: "TRIBUTI"})
	pub, _ := db.CreateAlert(database.Alert{Title: "per tutti", Level: database.LevelNews, StartsAt: fixedNow})
	ris, _ := db.CreateAlert(database.Alert{Title: "solo tributi", Level: database.LevelNews, StartsAt: fixedNow})
	db.SetContentAudience(database.ContentAlert, ris, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{trib}})
	vis := func(u string, id int64) bool { v, _ := s.alertVisibleTo(u, id); return v }
	if !vis("", pub) || vis("", ris) {
		t.Fatal("anonimo: solo avvisi pubblici")
	}
	if !vis("mrossi", ris) || vis("senzanome", ris) {
		t.Fatal("riservato: visibile al membro, non agli altri")
	}
	if _, unsure := s.alertVisibleTo("senzanome", ris); unsure {
		t.Fatal("utente sconosciuto ad AD: risposta certa (non è AD giù)")
	}
}

func TestAlertVisibleToADDown(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) { o.Directory = fakeDirectory{err: errors.New("giù")} })
	trib, _ := db.CreateAudienceGroup("Tributi")
	db.AddAudienceRule(database.AudienceRule{GroupID: trib, Kind: audience.KindUser, Value: "mrossi"})
	pub, _ := db.CreateAlert(database.Alert{Title: "per tutti", Level: database.LevelNews, StartsAt: fixedNow})
	ris, _ := db.CreateAlert(database.Alert{Title: "solo tributi", Level: database.LevelNews, StartsAt: fixedNow})
	db.SetContentAudience(database.ContentAlert, ris, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{trib}})
	if v, unsure := s.alertVisibleTo("mrossi", pub); !v || unsure {
		t.Fatal("avviso pubblico: visibile anche con AD giù")
	}
	if v, unsure := s.alertVisibleTo("mrossi", ris); v || !unsure {
		t.Fatalf("avviso riservato con AD giù: atteso incerto, ottenuto %v %v", v, unsure)
	}
	if _, unsure := s.alertVisibleTo("", ris); unsure {
		t.Fatal("anonimo: risposta certa anche con AD giù")
	}
}
