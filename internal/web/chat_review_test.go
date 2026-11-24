package web

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// partialDown: AD risponde per tutti tranne down (profilo scaduto in cache
// durante un guasto, mentre quello del destinatario è ancora valido).
type partialDown struct {
	fakeDirectory
	down string
}

func (d partialDown) Profile(u string, attrs []string) (audience.Profile, error) {
	if u == d.down {
		return audience.Profile{}, errAD
	}
	return d.fakeDirectory.Profile(u, attrs)
}

// Con AD giù per il mittente l'invio è rifiutato anche se ha fatto l'accesso:
// non si sa se è ancora nel pubblico della chat.
func TestChatSendSenderADDown(t *testing.T) {
	s, _ := newChatServer(t, func(o *Options) { o.Directory = partialDown{chatDirectory(), "mrossi"} })
	if m := chatSend(t, s, "mrossi", "abianchi", "ciao", true); m["errore"] != "ad" {
		t.Fatalf("invio con AD giù per il mittente: %v", m)
	}
}

// Il bloccato non se ne accorge: le sue altre schede ricevono l'evento come sempre.
func TestChatSendBlockedSenderStillRefreshed(t *testing.T) {
	s, db := newChatServer(t, nil)
	db.SetChatBlock("abianchi", "mrossi", true, fixedNow)
	c, _ := s.hub.Subscribe("mrossi")
	defer s.hub.Unsubscribe(c)
	if m := chatSend(t, s, "mrossi", "abianchi", "ciao", true); m["ok"] != true {
		t.Fatalf("invio: %v", m)
	}
	select {
	case e := <-c.Events:
		if e.Kind != "chat" || e.Title != "" {
			t.Fatalf("evento al mittente: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("il mittente bloccato non riceve l'evento delle sue schede")
	}
}

// Chat aperta a tutti: la ricerca dei colleghi non legge i profili da AD
// (una connessione per collega trovato).
func TestChatPeoplePublicNoProfiles(t *testing.T) {
	calls := 0
	dir := chatDirectory()
	dir.calls = &calls
	dir.members = append(dir.members, identity.Person{Username: "rmarino", Name: "Rossana Marino"})
	s, _ := newChatServer(t, func(o *Options) { o.Directory = dir })
	h := chatAs(t, s, "mrossi", false)
	do(t, s, "GET", "/chat/stato", nil, nil, h) // profilo di chi cerca già in cache
	before := calls
	m := chatJSON(t, do(t, s, "GET", "/chat/persone?q=ro", nil, nil, h))
	if m["ok"] != true || len(m["persone"].([]any)) == 0 {
		t.Fatalf("ricerca: %v", m)
	}
	if calls != before {
		t.Fatalf("letti %d profili per una chat pubblica", calls-before)
	}
}

// Una conversazione aperta in una scheda nascosta non si segna come letta:
// il badge resterebbe a zero senza che l'utente abbia visto nulla.
func TestChatJSReadOnlyVisible(t *testing.T) {
	b, err := os.ReadFile("../../web/static/js/chat.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	i := strings.Index(src, "async function markRead()")
	if i < 0 {
		t.Fatal("markRead non trovata")
	}
	body := src[i : i+strings.Index(src[i:], "\n\t}\n")]
	if !strings.Contains(body, `document.visibilityState !== "visible"`) {
		t.Fatalf("markRead senza controllo della visibilità:\n%s", body)
	}
	if !strings.Contains(src, `addEventListener("visibilitychange"`) {
		t.Fatal("chat.js non segna come letto al ritorno sulla scheda")
	}
}
