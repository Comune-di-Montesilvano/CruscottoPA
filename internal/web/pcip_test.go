package web

import (
	"context"
	"errors"
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
	if !strings.Contains(page, `<span class="hero-pc-ip">Indirizzo IP: 192.0.2.15</span>`) {
		t.Error("testata: IP del PC mancante")
	}
	if !strings.Contains(page, "IP <strong>192.0.2.15</strong>") {
		t.Error("dialog: IP mancante fra le informazioni allegate")
	}
	if out := postTicket(t, s, c, validTicket()); out["ok"] != true {
		t.Fatalf("invio: %v", out)
	}
	if !strings.Contains(m.Sent[0].Body, "PC: PC-PROVA-001\nIP: 192.0.2.15") {
		t.Fatalf("ticket senza IP: %q", m.Sent[0].Body)
	}
}
