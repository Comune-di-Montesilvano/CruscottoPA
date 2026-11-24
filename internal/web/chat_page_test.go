package web

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

const chatNotice = "Chat informale tra colleghi. Non usarla per comunicazioni ufficiali né per dati personali di cittadini. I messaggi si cancellano dopo 30 giorni. Per i casi di abuso restano registrati PC e indirizzo di rete di chi scrive: apri un ticket all'assistenza."

func TestChatWidgetDeclared(t *testing.T) {
	s, _ := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "uno", false)
	chatSend(t, s, "mrossi", "abianchi", "due", false)
	rec := do(t, s, "GET", "/partials/chat", nil, nil, chatAs(t, s, "abianchi", false))
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `id="widget-chat"`) || !strings.Contains(body, "2 messaggi nuovi") || !strings.Contains(body, "Accedi per leggerli") {
		t.Fatalf("widget non verificato: %d\n%s", rec.Code, body)
	}
	if strings.Contains(body, "Mario") || strings.Contains(body, "mrossi") {
		t.Fatalf("mittente visibile senza accesso:\n%s", body)
	}
}

func TestChatWidgetVerified(t *testing.T) {
	s, _ := newChatServer(t, nil)
	chatSend(t, s, "mrossi", "abianchi", "testo del messaggio", false)
	body := do(t, s, "GET", "/partials/chat", nil, nil, chatAs(t, s, "abianchi", true)).Body.String()
	if !strings.Contains(body, "Mario Rossi") || !strings.Contains(body, `data-chat-open="mrossi"`) || strings.Contains(body, "testo del messaggio") {
		t.Fatalf("widget verificato:\n%s", body)
	}
}

func TestChatWidgetHiddenOutside(t *testing.T) {
	s, db := newChatServer(t, nil)
	g := chatGroup(t, db, "Chat", "mrossi")
	db.SetContentAudience(database.ContentChat, database.ChatAudienceID, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{g}})
	body := do(t, s, "GET", "/", nil, nil, chatAs(t, s, "abianchi", false)).Body.String()
	if strings.Contains(body, `id="widget-chat"`) || strings.Contains(body, "data-chat-panel") {
		t.Fatal("chat in plancia per chi è fuori dal pubblico")
	}
	if rec := do(t, s, "GET", "/partials/chat", nil, nil, chatAs(t, s, "abianchi", false)); rec.Code != 200 || strings.Contains(rec.Body.String(), "widget-chat") {
		t.Fatalf("widget per chi è fuori: %d %s", rec.Code, rec.Body)
	}
	if body := do(t, s, "GET", "/", nil, nil, chatAs(t, s, "mrossi", false)).Body.String(); !strings.Contains(body, `id="widget-chat"`) || !strings.Contains(body, "data-chat-panel") {
		t.Fatal("chat assente per chi è dentro")
	}
	off, _ := newTestServer(t, nil)
	if body := do(t, off, "GET", "/", nil, nil, chatAs(t, off, "mrossi", false)).Body.String(); strings.Contains(body, "data-chat-panel") {
		t.Fatal("chat in plancia a chat spenta")
	}
}

func TestChatNoticeText(t *testing.T) {
	s, _ := newChatServer(t, nil)
	if body := do(t, s, "GET", "/", nil, nil, chatAs(t, s, "mrossi", false)).Body.String(); !strings.Contains(body, chatNotice) {
		t.Fatal("avviso fisso della chat assente o diverso")
	}
	body := do(t, s, "GET", "/chat?a=abianchi", nil, nil, chatAs(t, s, "mrossi", false)).Body.String()
	if !strings.Contains(body, chatNotice) || !strings.Contains(body, `data-open="abianchi"`) || !strings.Contains(body, "Accedi come <strong>Mario Rossi</strong>") {
		t.Fatalf("pagina /chat:\n%s", body)
	}
}

func TestChatPageUnavailable(t *testing.T) {
	s, _ := newChatServer(t, nil)
	rec := do(t, s, "GET", "/chat", nil, nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "La chat non è disponibile") {
		t.Fatalf("anonimo: %d\n%s", rec.Code, rec.Body)
	}
}

// Review Focus 4: il testo dei messaggi entra nel DOM solo come testo.
func TestChatJSSafeDOM(t *testing.T) {
	b, err := os.ReadFile("../../web/static/js/chat.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, bad := range []string{"innerHTML", "insertAdjacentHTML", "outerHTML", "document.write", "eval("} {
		if strings.Contains(src, bad) {
			t.Errorf("chat.js usa %s", bad)
		}
	}
	if !strings.Contains(src, "textContent") || !regexp.MustCompile(`protocol\s*===?\s*"https?:"`).MatchString(src) {
		t.Error("chat.js: manca textContent o il controllo del protocollo dei link")
	}
}

func TestDashboardJSChatOption(t *testing.T) {
	b, err := os.ReadFile("../../web/static/js/dashboard.js")
	if err != nil {
		t.Fatal(err)
	}
	if src := string(b); !strings.Contains(src, "p.chat && p.uid") || !strings.Contains(src, `"Scrivi a "`) {
		t.Fatal("dashboard.js senza la voce «Scrivi a» nelle persone")
	}
}

func TestNotificheJSChatEvent(t *testing.T) {
	b, err := os.ReadFile("../../web/static/js/notifiche.js")
	if err != nil {
		t.Fatal(err)
	}
	if src := string(b); !strings.Contains(src, `addEventListener("chat"`) || !strings.Contains(src, `"chat-"`) {
		t.Fatal("notifiche.js non gestisce l'evento chat")
	}
}

func TestAdminChatStats(t *testing.T) {
	s, db := newChatServer(t, nil)
	// login() entra come mrossi: il messaggio è tra altri due colleghi.
	c, _, _ := db.ChatConversationID("lverdi", "abianchi", true, fixedNow)
	db.AddChatMessage(database.ChatMessage{ConversationID: c, Sender: "lverdi", Body: "segreto", CreatedAt: fixedNow.Add(-time.Hour)})
	body := do(t, s, "GET", "/admin/chat", nil, login(t, s), nil).Body.String()
	for _, want := range []string{"Ultimi 30 giorni", "negli ultimi 7 giorni", "colleghi che hanno scritto"} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	for _, bad := range []string{"segreto", "lverdi", "abianchi", "Luca", "Anna"} {
		if strings.Contains(body, bad) {
			t.Errorf("dato personale nella pagina: %q", bad)
		}
	}
}

func TestChatEmojiPicker(t *testing.T) {
	s, _ := newChatServer(t, nil)
	body := do(t, s, "GET", "/chat?a=abianchi", nil, nil, chatAs(t, s, "mrossi", true)).Body.String()
	if !strings.Contains(body, "data-chat-emoji") || !strings.Contains(body, "data-chat-emoji-panel") {
		t.Fatal("selettore emoji assente")
	}
	if !strings.Contains(body, `class="chat-logout" data-chat-logout`) || strings.Contains(body, "Esci dalla chat") {
		t.Fatal("logout non spostato in fondo all'elenco")
	}
	if !strings.Contains(body, "data-chat-notice-close") {
		t.Fatal("avviso non chiudibile")
	}
	b, _ := os.ReadFile("../../web/static/js/chat.js")
	if src := string(b); !strings.Contains(src, "NOTICE_DAYS = 7") || !strings.Contains(src, "localStorage") {
		t.Fatal("avviso chiuso senza ritorno dopo 7 giorni")
	}
	if src := string(b); !strings.Contains(src, "POLL_MS") || !strings.Contains(src, "lastId(d) !== current.lastId") {
		t.Fatal("chat.js senza controllo periodico dei messaggi nuovi")
	}
	if src := string(b); !strings.Contains(src, "RECENT_KEY") || !strings.Contains(src, "Lavoro e oggetti") {
		t.Fatal("emoji senza categorie e recenti")
	}
	if src := string(b); !strings.Contains(src, "setRangeText") || !strings.Contains(src, "EMOJI") {
		t.Fatal("chat.js non inserisce le emoji nel testo")
	}
}
