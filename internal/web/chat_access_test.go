package web

import (
	"net/url"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestChatAllowedPublic(t *testing.T) {
	s, _ := newChatServer(t, nil)
	if ok, down := s.chatAllowed("mrossi"); !ok || down {
		t.Fatalf("pubblico: %v %v", ok, down)
	}
	if ok, _ := s.chatAllowed("sconosciuto"); ok {
		t.Fatal("utente sconosciuto ad AD ammesso")
	}
	if ok, _ := s.chatAllowed(""); ok {
		t.Fatal("username vuoto ammesso")
	}
}

func TestChatAllowedGroups(t *testing.T) {
	s, db := newChatServer(t, nil)
	g := chatGroup(t, db, "Chat", "mrossi")
	db.SetContentAudience(database.ContentChat, database.ChatAudienceID, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{g}})
	if ok, _ := s.chatAllowed("mrossi"); !ok {
		t.Fatal("membro escluso")
	}
	if ok, _ := s.chatAllowed("abianchi"); ok {
		t.Fatal("non membro ammesso")
	}
	db.SetContentAudience(database.ContentChat, database.ChatAudienceID, database.ContentAudience{Mode: audience.ModeHide, Groups: []int64{g}})
	if ok, _ := s.chatAllowed("mrossi"); ok {
		t.Fatal("nascosto a mrossi ma ammesso")
	}
	if ok, _ := s.chatAllowed("abianchi"); !ok {
		t.Fatal("abianchi escluso con hide")
	}
}

func TestChatAllowedADDown(t *testing.T) {
	s, _ := newChatServer(t, func(o *Options) {
		d := chatDirectory()
		d.err = errAD
		o.Directory = d
	})
	if ok, down := s.chatAllowed("mrossi"); ok || !down {
		t.Fatalf("AD giù: %v %v", ok, down)
	}
}

func TestAdminChatSave(t *testing.T) {
	s, db := newChatServer(t, nil)
	g := chatGroup(t, db, "CED", "mrossi")
	c := login(t, s)
	if rec := do(t, s, "GET", "/admin/chat", nil, c, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Chi è fuori non può scrivere né ricevere messaggi") {
		t.Fatalf("pagina: %d\n%s", rec.Code, rec.Body)
	}
	rec := do(t, s, "POST", "/admin/chat", url.Values{"visibilita": {"only"}, "gruppi": {itoa(g)}}, c, hx)
	if rec.Code != 200 || invalid(rec) {
		t.Fatalf("salvataggio: %d %v\n%s", rec.Code, rec.Header(), rec.Body)
	}
	ca, _ := db.GetContentAudience(database.ContentChat, database.ChatAudienceID)
	if ca.Mode != audience.ModeOnly || len(ca.Groups) != 1 || ca.Groups[0] != g {
		t.Fatalf("pubblico salvato: %+v", ca)
	}
	log := auditRows(t, db)
	if len(log) == 0 || log[0].Section != "Chat" || log[0].Action != "modificato" {
		t.Fatalf("registro: %+v", log)
	}
	if rec := do(t, s, "POST", "/admin/chat", url.Values{"visibilita": {"only"}}, c, hx); !invalid(rec) {
		t.Fatalf("senza gruppi: %d %v", rec.Code, rec.Header())
	}
}

func TestChatRoutesOffWhenDisabled(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	if rec := do(t, s, "GET", "/admin/chat", nil, c, nil); rec.Code != 404 {
		t.Fatalf("/admin/chat a chat spenta: %d", rec.Code)
	}
	if rec := do(t, s, "GET", "/admin", nil, c, nil); strings.Contains(rec.Body.String(), `href="/admin/chat"`) {
		t.Fatal("voce Chat nel menu a chat spenta")
	}
	s2, _ := newChatServer(t, nil)
	if rec := do(t, s2, "GET", "/admin", nil, login(t, s2), nil); !strings.Contains(rec.Body.String(), `href="/admin/chat"`) {
		t.Fatal("voce Chat assente a chat attiva")
	}
}
