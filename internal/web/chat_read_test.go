package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// chatAs: cookie utente, più quello della chat se verified.
func chatAs(t *testing.T, s *Server, user string, verified bool) map[string]string {
	t.Helper()
	cs := []*http.Cookie{userCookie(t, s, user, "")}
	if verified {
		cs = append(cs, chatCookie(t, s, user))
	}
	return withCookies(nil, cs...)
}

func chatGet(t *testing.T, s *Server, path, user string, verified bool) (map[string]any, string) {
	t.Helper()
	rec := do(t, s, "GET", path, nil, nil, chatAs(t, s, user, verified))
	body := rec.Body.String()
	return chatJSON(t, rec), body
}

func chatConvID(t *testing.T, db *database.DB, a, b string) string {
	t.Helper()
	id, _, err := db.ChatConversationID(a, b, false, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	return itoa(id)
}

func TestChatReadNeedsVerified(t *testing.T) {
	s, db := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "testo riservato", false)
	c := chatConvID(t, db, "mrossi", "abianchi")
	for _, p := range []string{"/chat/conversazioni", "/chat/conversazioni/" + c, "/chat/con/mrossi"} {
		m, body := chatGet(t, s, p, "abianchi", false)
		if strings.Contains(body, "testo riservato") {
			t.Fatalf("%s: testo senza accesso\n%s", p, body)
		}
		if p != "/chat/con/mrossi" && m["errore"] != "accesso" {
			t.Fatalf("%s: %v", p, m)
		}
	}
	// Non verificato: si può scrivere a un collega, senza vedere i messaggi.
	if m, _ := chatGet(t, s, "/chat/con/mrossi", "abianchi", false); m["ok"] != true || m["nome"] != "Mario Rossi" || m["accesso"] != false {
		t.Fatalf("con, non verificato: %v", m)
	}
}

func TestChatReadVerified(t *testing.T) {
	s, db := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "primo", false)
	chatSend(t, s, "mrossi", "abianchi", "secondo", true)
	m, _ := chatGet(t, s, "/chat/conversazioni", "abianchi", true)
	cs := m["conversazioni"].([]any)
	if len(cs) != 1 {
		t.Fatalf("conversazioni: %v", m)
	}
	conv := cs[0].(map[string]any)
	if conv["con"] != "mrossi" || conv["nome"] != "Mario Rossi" || conv["nuovi"] != float64(2) {
		t.Fatalf("conversazione: %v", conv)
	}
	th, _ := chatGet(t, s, "/chat/conversazioni/"+chatConvID(t, db, "mrossi", "abianchi"), "abianchi", true)
	msgs := th["messaggi"].([]any)
	first, second := msgs[0].(map[string]any), msgs[1].(map[string]any)
	if len(msgs) != 2 || first["testo"] != "primo" || first["verificato"] != false || first["mio"] != false || second["verificato"] != true || th["altri"] != false {
		t.Fatalf("thread: %v", th)
	}
	byUser, _ := chatGet(t, s, "/chat/con/mrossi", "abianchi", true)
	if len(byUser["messaggi"].([]any)) != 2 || byUser["accesso"] != true {
		t.Fatalf("con: %v", byUser)
	}
	do(t, s, "POST", "/chat/conversazioni/"+chatConvID(t, db, "mrossi", "abianchi")+"/letto", nil, nil, chatAs(t, s, "abianchi", true))
	if n, _ := db.ChatUnreadTotal("abianchi"); n != 0 {
		t.Fatalf("non letti dopo letto: %d", n)
	}
}

func TestChatReadNoIPOrPC(t *testing.T) {
	s, db := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "ciao", false)
	c := chatConvID(t, db, "mrossi", "abianchi")
	for _, p := range []string{"/chat/conversazioni", "/chat/conversazioni/" + c, "/chat/con/mrossi"} {
		if _, body := chatGet(t, s, p, "abianchi", true); strings.Contains(body, "192.0.2.15") || strings.Contains(body, "PC-PROVA-001") {
			t.Fatalf("%s: IP o PC nella risposta\n%s", p, body)
		}
	}
}

func TestChatReadOtherConversation(t *testing.T) {
	s, db := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "privato", false)
	m, body := chatGet(t, s, "/chat/conversazioni/"+chatConvID(t, db, "mrossi", "abianchi"), "lverdi", true)
	if m["errore"] != "conversazione" || strings.Contains(body, "privato") {
		t.Fatalf("estraneo: %v", m)
	}
}

// Review Focus 3: con AD giù chi ha fatto l'accesso legge ancora.
func TestChatReadADDownStillWorks(t *testing.T) {
	d := chatDirectory()
	s, db := newChatServer(t, func(o *Options) { o.Directory = d })
	chatSend(t, s, "mrossi", "abianchi", "ciao", false)
	c := chatConvID(t, db, "mrossi", "abianchi")
	down := chatDirectory()
	down.err = errAD
	s2, _ := newChatServer(t, func(o *Options) { o.Directory = down; o.DB = db })
	if m, _ := chatGet(t, s2, "/chat/conversazioni/"+c, "abianchi", true); m["ok"] != true {
		t.Fatalf("AD giù, verificato: %v", m)
	}
	if m, _ := chatGet(t, s2, "/chat/conversazioni/"+c, "abianchi", false); m["ok"] == true {
		t.Fatalf("AD giù, non verificato: %v", m)
	}
}

func TestChatOutsideAudienceCannotRead(t *testing.T) {
	s, db := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "ciao", false)
	g := chatGroup(t, db, "Chat", "mrossi")
	db.SetContentAudience(database.ContentChat, database.ChatAudienceID, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{g}})
	if m, body := chatGet(t, s, "/chat/conversazioni", "abianchi", true); m["ok"] == true || strings.Contains(body, "mrossi") {
		t.Fatalf("fuori dal pubblico: %v", m)
	}
}

func TestChatStatoCount(t *testing.T) {
	s, _ := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "uno", false)
	chatSend(t, s, "mrossi", "abianchi", "due", false)
	m, body := chatGet(t, s, "/chat/stato", "abianchi", false)
	if m["ok"] != true || m["nuovi"] != float64(2) || m["verificato"] != false || strings.Contains(body, "mrossi") || strings.Contains(body, "Mario") {
		t.Fatalf("stato: %s", body)
	}
	if m, _ := chatGet(t, s, "/chat/stato", "abianchi", true); m["verificato"] != true {
		t.Fatalf("stato verificato: %v", m)
	}
}

func TestChatBlockRoute(t *testing.T) {
	s, db := newChatServer(t, nil)
	if m := chatJSON(t, do(t, s, "POST", "/chat/blocchi", url.Values{"utente": {"mrossi"}, "blocca": {"1"}}, nil, chatAs(t, s, "abianchi", false))); m["errore"] != "accesso" {
		t.Fatalf("blocco senza accesso: %v", m)
	}
	chatJSON(t, do(t, s, "POST", "/chat/blocchi", url.Values{"utente": {"MRossi"}, "blocca": {"1"}}, nil, chatAs(t, s, "abianchi", true)))
	if b, _ := db.ChatBlocked("abianchi", "mrossi"); !b {
		t.Fatal("blocco non salvato")
	}
	m, _ := chatGet(t, s, "/chat/con/mrossi", "abianchi", true)
	if m["bloccato"] != true {
		t.Fatalf("bloccato nel thread: %v", m)
	}
	chatJSON(t, do(t, s, "POST", "/chat/blocchi", url.Values{"utente": {"mrossi"}, "blocca": {"0"}}, nil, chatAs(t, s, "abianchi", true)))
	if b, _ := db.ChatBlocked("abianchi", "mrossi"); b {
		t.Fatal("sblocco non salvato")
	}
}

func TestChatPersoneFiltered(t *testing.T) {
	s, db := newChatServer(t, nil)
	g := chatGroup(t, db, "Chat", "mrossi", "abianchi")
	db.SetContentAudience(database.ContentChat, database.ChatAudienceID, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{g}})
	m, body := chatGet(t, s, "/chat/persone?q=ro", "mrossi", false)
	ps := m["persone"].([]any)
	if len(ps) != 1 || ps[0].(map[string]any)["utente"] != "abianchi" || strings.Contains(body, "lverdi") {
		t.Fatalf("persone: %s", body)
	}
	if m, _ := chatGet(t, s, "/chat/persone?q=r", "mrossi", false); len(m["persone"].([]any)) != 0 {
		t.Fatalf("un carattere: %v", m)
	}
}

func TestRubricaChatFlag(t *testing.T) {
	fake := func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"uid":"abianchi","nome":"Anna Bianchi"},{"uid":"esterno","nome":"Esterno"}]`))
	}
	s, _ := rubricaServer(t, fake)
	s.cfg.ChatEnabled = true
	s.directory = chatDirectory()
	rec := do(t, s, "GET", "/rubrica/cerca?q=bia", nil, nil, chatAs(t, s, "mrossi", false))
	body := rec.Body.String()
	if !strings.Contains(body, `"uid":"abianchi","nome":"Anna Bianchi","reparto":"","interno":"","mail":"","url":"`) || !strings.Contains(body, `"chat":true`) || !strings.Contains(body, `"uid":"esterno"`) {
		t.Fatalf("risposta: %s", body)
	}
	if strings.Count(body, `"chat":true`) != 1 {
		t.Fatalf("chat per chi non è in AD: %s", body)
	}
	off, _ := rubricaServer(t, fake)
	if body := do(t, off, "GET", "/rubrica/cerca?q=bia", nil, nil, chatAs(t, off, "mrossi", false)).Body.String(); strings.Contains(body, `"chat":true`) {
		t.Fatalf("chat spenta: %s", body)
	}
}
