package web

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity/ntlmtest"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

// Il nome del PC arriva dal messaggio NTLM (workstation): dichiarato, serve
// solo come etichetta per l'assistenza.
func TestIoStoresWorkstation(t *testing.T) {
	s, _ := newTestServer(t, nil)
	for in, want := range map[string]string{
		"PC-PROVA-001":          "PC-PROVA-001",
		"pc<script>x":           "pcscriptx",
		strings.Repeat("A", 80): strings.Repeat("A", 63),
		"":                      "",
	} {
		rec := do(t, s, "GET", "/io", nil, nil, ntlmHeader(ntlmtest.Authenticate("COMUNE-MS", "mrossi", in)))
		c := cookieNamed(rec, identity.CookieName)
		if c == nil {
			t.Fatalf("%q: nessun cookie", in)
		}
		if u, ok := s.cookies.Decode(c.Value); !ok || u.PC != want {
			t.Errorf("workstation %q: PC %q, atteso %q", in, u.PC, want)
		}
	}
}

// /io?aggiorna=1 (una volta per sessione, per chi è già riconosciuto): se il
// browser non completa l'handshake o AD è giù, il cookie resta com'è.
func TestIoRefreshNeverDowngrades(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "GET", "/io?aggiorna=1", nil, nil, nil)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != "NTLM" || cookieNamed(rec, identity.CookieName) != nil {
		t.Fatalf("aggiorna, passo 0: %d %v", rec.Code, rec.Result().Cookies())
	}
	down, _ := newTestServerWith(t, nil, func(o *Options) { o.Directory = fakeDirectory{err: errors.New("giù")} })
	rec = do(t, down, "GET", "/io?aggiorna=1", nil, nil, ntlmHeader(ntlmtest.Authenticate("COMUNE-MS", "mrossi", "PC-1")))
	if c := cookieNamed(rec, identity.CookieName); c != nil {
		t.Fatalf("aggiorna con AD giù: cookie toccato %+v", c)
	}
	rec = do(t, s, "GET", "/io?aggiorna=1", nil, nil, ntlmHeader(ntlmtest.Authenticate("COMUNE-MS", "mrossi", "PC-2")))
	c := cookieNamed(rec, identity.CookieName)
	if u, ok := s.cookies.Decode(c.Value); rec.Code != 200 || !ok || u.PC != "PC-2" || u.Name != "Mario Rossi" {
		t.Fatalf("aggiorna riuscito: %d %+v", rec.Code, u)
	}
}

func TestDashboardShowsPC(t *testing.T) {
	s, _ := newTestServer(t, nil)
	with := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi", PC: "PC-PROVA-001"}), nil).Body.String()
	for _, want := range []string{"data-pc", "PC-PROVA-001", "data-pc-copy", "data-pc-aggiorna"} {
		if !strings.Contains(with, want) {
			t.Errorf("con PC: manca %q", want)
		}
	}
	without := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"}), nil).Body.String()
	if strings.Contains(without, "data-pc-copy") {
		t.Error("senza PC: elemento presente")
	}
	if !strings.Contains(without, "data-pc-aggiorna") {
		t.Error("utente riconosciuto senza PC: deve comunque aggiornare il nome")
	}
	if anon := do(t, s, "GET", "/", nil, nil, nil).Body.String(); strings.Contains(anon, "data-pc-aggiorna") {
		t.Error("anonimo: niente aggiornamento del PC")
	}
}

func TestTicketIncludesPCInfo(t *testing.T) {
	m := &otrs.Mock{}
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Directory = ticketDirectory; o.Tickets = m })
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi", PC: "PC-PROVA-001"})
	page := do(t, s, "GET", "/", nil, c, nil).Body.String()
	if !strings.Contains(page, "data-ticket-pc") {
		t.Error("il dialog deve mostrare le informazioni sul PC che verranno inviate")
	}
	f := validTicket()
	f.Set("browser", "Edge 141")
	f.Set("sistema", "Windows 11\nX-Iniezione: 1")
	f.Set("schermo", strings.Repeat("9", 100))
	f.Set("pc", "ALTRO-PC") // ignorato: vale quello del cookie
	if out := postTicket(t, s, c, f); out["ok"] != true {
		t.Fatalf("invio: %v", out)
	}
	body := m.Sent[0].Body
	want := "Non stampa più.\nÈ urgente\n\n— Informazioni sul PC —\nPC: PC-PROVA-001\nBrowser: Edge 141\nSistema: Windows 11 X-Iniezione: 1\nSchermo: " + strings.Repeat("9", 60)
	if body != want {
		t.Fatalf("corpo:\n%q\natteso:\n%q", body, want)
	}
	list, _ := s.db.ListTickets(10)
	if list[0].PC != "PC-PROVA-001" {
		t.Fatalf("registro: %+v", list[0])
	}
	admin := do(t, s, "GET", "/admin/ticket", nil, login(t, s), nil).Body.String()
	if !strings.Contains(admin, "PC-PROVA-001") {
		t.Error("pagina admin senza il nome del PC")
	}
}

// Senza nome del PC (es. riconosciuto prima di questa versione) il blocco
// riporta comunque browser, sistema e schermo.
func TestTicketPCInfoWithoutPCName(t *testing.T) {
	m := &otrs.Mock{}
	s, c := ticketTestServer(t, m)
	f := validTicket()
	f.Set("browser", "Chrome 140")
	if out := postTicket(t, s, c, f); out["ok"] != true {
		t.Fatalf("invio: %v", out)
	}
	if !strings.HasSuffix(m.Sent[0].Body, "— Informazioni sul PC —\nPC: non rilevato\nBrowser: Chrome 140") {
		t.Fatalf("corpo: %q", m.Sent[0].Body)
	}
	if list, _ := s.db.ListTickets(1); list[0].PC != "" {
		t.Fatalf("registro: %+v", list[0])
	}
}

func TestPCScripts(t *testing.T) {
	dash, _ := os.ReadFile("../../web/static/js/dashboard.js")
	for _, want := range []string{"/io?aggiorna=1", "data-pc-aggiorna", "data-pc-copy", "clipboard"} {
		if !strings.Contains(string(dash), want) {
			t.Errorf("dashboard.js: manca %q", want)
		}
	}
	tk, _ := os.ReadFile("../../web/static/js/ticket.js")
	for _, want := range []string{"userAgentData", "schermo", "sistema", "browser", "data-pc-info"} {
		if !strings.Contains(string(tk), want) {
			t.Errorf("ticket.js: manca %q", want)
		}
	}
}

var _ = database.TicketSent{}
