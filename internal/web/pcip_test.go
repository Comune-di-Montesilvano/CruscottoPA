package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

type fakeResolver struct {
	ips   map[string][]string
	calls *int
}

func (f fakeResolver) lookup(_ context.Context, host string) ([]string, error) {
	*f.calls++
	if ips, ok := f.ips[host]; ok {
		return ips, nil
	}
	return nil, errors.New("NXDOMAIN")
}

func pcipServer(t *testing.T, m otrs.Client) (*Server, *int) {
	t.Helper()
	calls := 0
	r := fakeResolver{ips: map[string][]string{"pc-prova-001.intranet.example.it": {"fe80::1", "192.0.2.15"}}, calls: &calls}
	s, _ := newTestServerWith(t, nil, func(o *Options) {
		o.Directory = ticketDirectory
		o.Tickets = m
		o.Config.PCDNSSuffix = "intranet.example.it"
		o.PCLookup = r.lookup
	})
	return s, &calls
}

// IP del PC dal DNS del dominio (nome NTLM + suffisso): solo IPv4, in cache.
func TestPCIPLookupAndCache(t *testing.T) {
	s, calls := pcipServer(t, otrs.NewMock())
	if ip := s.pcIP("PC-PROVA-001"); ip != "192.0.2.15" {
		t.Fatalf("IP: %q", ip)
	}
	s.pcIP("pc-prova-001")
	if *calls != 1 {
		t.Fatalf("DNS interrogato %d volte, atteso 1 (cache)", *calls)
	}
	if ip := s.pcIP("PC-SCONOSCIUTO"); ip != "" {
		t.Fatalf("PC sconosciuto: %q", ip)
	}
	if ip := s.pcIP(""); ip != "" || *calls != 2 {
		t.Fatalf("senza nome: %q, chiamate %d", ip, *calls)
	}
}

func TestPCIPDisabledWithoutSuffix(t *testing.T) {
	calls := 0
	s, _ := newTestServerWith(t, nil, func(o *Options) {
		o.PCLookup = fakeResolver{ips: map[string][]string{}, calls: &calls}.lookup
	})
	if ip := s.pcIP("PC-PROVA-001"); ip != "" || calls != 0 {
		t.Fatalf("senza PC_DNS_SUFFIX: %q, chiamate %d", ip, calls)
	}
}

func TestPCIPInHeaderAndTicket(t *testing.T) {
	m := otrs.NewMock()
	s, _ := pcipServer(t, m)
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi", PC: "PC-PROVA-001"})
	page := do(t, s, "GET", "/", nil, c, nil).Body.String()
	for _, want := range []string{
		`<strong class="hero-pc-name">PC-PROVA-001</strong>`,
		`<span class="hero-pc-ip">Indirizzo IP 192.0.2.15</span>`,
		`data-pc-copy="PC-PROVA-001" aria-label="Copia il nome del PC"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("testata: manca %s", want)
		}
	}
	if strings.Contains(page, `data-pc-copy="192.0.2.15"`) {
		t.Error("testata: l'IP non ha il bottone «Copia»")
	}
	if !strings.Contains(page, "IP <strong>192.0.2.15</strong>") {
		t.Error("dialog: IP mancante fra le informazioni allegate")
	}
	if !strings.Contains(page, "dal tuo PC:<br><strong>PC-PROVA-001</strong> · IP") || strings.Contains(page, "PC <strong>PC-PROVA-001") {
		t.Error("dialog: «PC» ripetuto davanti al nome")
	}
	if out := postTicket(t, s, c, validTicket()); out["ok"] != true {
		t.Fatalf("invio: %v", out)
	}
	if !strings.Contains(m.Sent[0].Body, "PC: PC-PROVA-001\nIP: 192.0.2.15") {
		t.Fatalf("ticket senza IP: %q", m.Sent[0].Body)
	}
}

const testProxyToken = "segreto-di-prova-0123456789"

func realIPServer(t *testing.T, m otrs.Client) *Server {
	t.Helper()
	s, _ := pcipServer(t, m)
	s.cfg.ProxyToken = testProxyToken
	return s
}

func proxyHeaders(ip, token string) map[string]string {
	return map[string]string{"X-Real-Ip": ip, "X-Proxy-Token": token, "Sec-Fetch-Site": "same-origin"}
}

// IP della connessione: solo con il token del proxy, solo IPv4.
func TestRealIPNeedsToken(t *testing.T) {
	s := realIPServer(t, otrs.NewMock())
	for _, c := range []struct {
		name, ip, token, want string
	}{
		{"token giusto", "192.0.2.30", testProxyToken, "192.0.2.30"},
		{"spazi", " 192.0.2.30 ", testProxyToken, "192.0.2.30"},
		{"token sbagliato", "192.0.2.30", "altro", ""},
		{"senza token", "192.0.2.30", "", ""},
		{"IPv6", "2001:db8::1", testProxyToken, ""},
		{"non IP", "pc-prova", testProxyToken, ""},
		{"elenco", "192.0.2.30, 192.0.2.31", testProxyToken, ""},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		for k, v := range proxyHeaders(c.ip, c.token) {
			r.Header.Set(k, v)
		}
		if got := s.realIP(r); got != c.want {
			t.Errorf("%s: %q, atteso %q", c.name, got, c.want)
		}
	}
	s.cfg.ProxyToken = ""
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Real-Ip", "192.0.2.30")
	r.Header.Set("X-Proxy-Token", "")
	if got := s.realIP(r); got != "" {
		t.Errorf("PROXY_TOKEN vuoto: %q", got)
	}
}

// Testata e dialog: l'IP della connessione vince sul DNS.
func TestRealIPPreferredInHeader(t *testing.T) {
	s := realIPServer(t, otrs.NewMock())
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi", PC: "PC-PROVA-001"})
	page := do(t, s, "GET", "/", nil, c, proxyHeaders("192.0.2.30", testProxyToken)).Body.String()
	if !strings.Contains(page, `<span class="hero-pc-ip">Indirizzo IP 192.0.2.30</span>`) || !strings.Contains(page, "IP <strong>192.0.2.30</strong>") {
		t.Fatal("testata o dialog senza l'IP della connessione")
	}
	if strings.Contains(page, "192.0.2.15") || strings.Contains(page, "Attenzione") {
		t.Fatal("l'IP del DNS o l'incongruenza non vanno mostrati all'utente")
	}
}

func postTicketWith(t *testing.T, s *Server, c *http.Cookie, path string, form url.Values, h map[string]string) map[string]any {
	t.Helper()
	rec := do(t, s, "POST", path, form, c, h)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("JSON non valido: %s", rec.Body)
	}
	return out
}

// Ticket: IP della connessione e, se il DNS dice altro, una riga per l'operatore.
func TestTicketFlagsDNSMismatch(t *testing.T) {
	m := otrs.NewMock()
	s := realIPServer(t, m)
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi", PC: "PC-PROVA-001"})
	if out := postTicketWith(t, s, c, "/ticket", validTicket(), proxyHeaders("192.0.2.30", testProxyToken)); out["ok"] != true {
		t.Fatalf("invio: %v", out)
	}
	want := "PC: PC-PROVA-001\nIP: 192.0.2.30\nAttenzione: il DNS indica 192.0.2.15 per questo PC (VPN, cambio di rete o record DNS vecchio)"
	if !strings.Contains(m.Sent[0].Body, want) {
		t.Fatalf("ticket senza incongruenza: %q", m.Sent[0].Body)
	}

	// Stesso IP: nessuna riga.
	if out := postTicketWith(t, s, c, "/ticket", validTicket(), proxyHeaders("192.0.2.15", testProxyToken)); out["ok"] != true {
		t.Fatalf("invio: %v", out)
	}
	if b := m.Sent[1].Body; !strings.Contains(b, "IP: 192.0.2.15") || strings.Contains(b, "Attenzione") {
		t.Fatalf("IP coincidenti: %q", b)
	}

	// Senza token: solo il DNS, come prima.
	if out := postTicketWith(t, s, c, "/ticket", validTicket(), proxyHeaders("192.0.2.30", "altro")); out["ok"] != true {
		t.Fatalf("invio: %v", out)
	}
	if b := m.Sent[2].Body; !strings.Contains(b, "IP: 192.0.2.15") || strings.Contains(b, "192.0.2.30") || strings.Contains(b, "Attenzione") {
		t.Fatalf("token sbagliato: %q", b)
	}
}

func TestPCBlockMismatch(t *testing.T) {
	r := httptest.NewRequest("POST", "/ticket", nil)
	if b := pcBlock("PC-PROVA-001", pcAddr{IP: "192.0.2.30", DNS: "192.0.2.15"}, r); !strings.Contains(b, "IP: 192.0.2.30\nAttenzione: il DNS indica 192.0.2.15") {
		t.Fatalf("incongruenza: %q", b)
	}
	if b := pcBlock("PC-PROVA-001", pcAddr{IP: "192.0.2.30"}, r); strings.Contains(b, "Attenzione") {
		t.Fatalf("senza DNS non c'è incongruenza: %q", b)
	}
}
