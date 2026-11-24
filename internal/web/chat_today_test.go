package web

import (
	"net/url"
	"strings"
	"testing"
)

func convToday(t *testing.T, s *Server, viewer, other string) any {
	t.Helper()
	m, _ := chatGet(t, s, "/chat/conversazioni", viewer, true)
	for _, c := range m["conversazioni"].([]any) {
		if c := c.(map[string]any); c["con"] == other {
			return c["oggi"]
		}
	}
	t.Fatalf("conversazione con %s assente: %v", other, m)
	return nil
}

func TestChatTodayStatus(t *testing.T) {
	s, _ := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "ciao", false) // inviare conta
	if v := convToday(t, s, "abianchi", "mrossi"); v != true {
		t.Fatalf("mrossi oggi in chat: %v", v)
	}
	m, _ := chatGet(t, s, "/chat/con/mrossi", "abianchi", true)
	if m["oggi"] != true {
		t.Fatalf("testata della conversazione: %v", m)
	}
	// Il widget che si aggiorna da solo non conta.
	do(t, s, "GET", "/partials/chat", nil, nil, chatAs(t, s, "lverdi", true))
	chatSend(t, s, "abianchi", "lverdi", "ciao", false)
	if v := convToday(t, s, "abianchi", "lverdi"); v != false {
		t.Fatalf("lverdi contato dal solo widget: %v", v)
	}
}

func TestChatTodayHiddenReciprocal(t *testing.T) {
	s, _ := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "ciao", false)
	chatSend(t, s, "abianchi", "mrossi", "ciao", false)
	m := chatJSON(t, do(t, s, "POST", "/chat/visibilita", url.Values{"nascondi": {"1"}}, nil, chatAs(t, s, "mrossi", false)))
	if m["ok"] != true {
		t.Fatalf("nascondi: %v", m)
	}
	if v := convToday(t, s, "abianchi", "mrossi"); v != false {
		t.Fatalf("stato nascosto visibile agli altri: %v", v)
	}
	if v := convToday(t, s, "mrossi", "abianchi"); v != false {
		t.Fatalf("chi nasconde il suo vede quello degli altri: %v", v)
	}
	body := do(t, s, "GET", "/chat", nil, nil, chatAs(t, s, "mrossi", true)).Body.String()
	if !strings.Contains(body, "data-chat-visibility") || strings.Contains(body, `data-chat-visibility checked`) {
		t.Fatal("scelta «Mostra quando sono in chat» non spenta nel pannello")
	}
}

func TestChatTodayAdminSwitch(t *testing.T) {
	s, db := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "ciao", false)
	rec := do(t, s, "POST", "/admin/chat/stato", url.Values{}, login(t, s), hx) // checkbox non spuntata = spento
	if rec.Code != 200 || invalid(rec) {
		t.Fatalf("interruttore: %d", rec.Code)
	}
	if on, _ := db.ChatStatusEnabled(); on {
		t.Fatal("interruttore non spento")
	}
	if v := convToday(t, s, "abianchi", "mrossi"); v != false {
		t.Fatalf("stato con interruttore spento: %v", v)
	}
	if rows := auditRows(t, db); len(rows) == 0 || rows[0].Section != "Chat" || rows[0].Title != "Stato «Oggi in chat»" {
		t.Fatalf("registro: %+v", rows)
	}
}

func TestChatTodayPeople(t *testing.T) {
	s, _ := newChatServer(t, nil)
	chatSend(t, s, "abianchi", "lverdi", "ciao", false)
	m, _ := chatGet(t, s, "/chat/persone?q=an", "mrossi", false)
	for _, p := range m["persone"].([]any) {
		if p := p.(map[string]any); p["utente"] == "abianchi" && p["oggi"] != true {
			t.Fatalf("ricerca: %v", p)
		}
	}
}

// Con la chat attiva il badge «Notifiche disattivate» elenca anche i messaggi.
func TestNotifyOffMentionsChat(t *testing.T) {
	s, _ := newChatServer(t, nil)
	body := do(t, s, "GET", "/", nil, nil, chatAs(t, s, "mrossi", true)).Body.String()
	if !strings.Contains(body, "Non riceverai gli avvisi urgenti e i messaggi della chat") &&
		!strings.Contains(body, "Non riceverai gli avvisi urgenti, i messaggi della chat e le risposte ai ticket") {
		t.Fatal("badge delle notifiche senza la chat")
	}
}

// Omonimi: sotto il nome l'ufficio da AD (department).
func TestChatDetailForHomonyms(t *testing.T) {
	s, _ := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "ciao", false)
	m, _ := chatGet(t, s, "/chat/conversazioni", "abianchi", true)
	if c := m["conversazioni"].([]any)[0].(map[string]any); c["dettaglio"] != "Ragioneria" {
		t.Fatalf("conversazioni: %v", c)
	}
	if m, _ := chatGet(t, s, "/chat/con/mrossi", "abianchi", true); m["dettaglio"] != "Ragioneria" {
		t.Fatalf("testata: %v", m)
	}
	if m, _ := chatGet(t, s, "/chat/persone?q=an", "mrossi", false); len(m["persone"].([]any)) == 0 ||
		m["persone"].([]any)[0].(map[string]any)["dettaglio"] != "Ragioneria" {
		t.Fatalf("persone: %v", m)
	}
}

// Widget: «Messaggi» apre il pannello; ufficio, contatore compatto e pallino.
func TestChatWidgetTouches(t *testing.T) {
	s, _ := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "ciao", false)
	body := do(t, s, "GET", "/partials/chat", nil, nil, chatAs(t, s, "abianchi", true)).Body.String()
	for _, want := range []string{
		`<button type="button" class="cw-title" data-chat-open="">Messaggi</button>`,
		`<span class="cw-sub">Ragioneria`,
		`class="cw-count" aria-label="1 messaggio nuovo">1</span>`,
		`class="chat-avatar is-today"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("widget senza %q:\n%s", want, body)
		}
	}
}
