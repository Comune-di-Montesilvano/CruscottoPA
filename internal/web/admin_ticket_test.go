package web

import (
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

func TestOTRSAgentBase(t *testing.T) {
	for in, want := range map[string]string{
		"https://otrs.example.it/otrs/nph-genericinterface.pl/Webservice/Prova": "https://otrs.example.it/otrs/",
		"mock":                      "",
		"https://otrs.example.it/x": "",
	} {
		if got := otrsAgentBase(in); got != want {
			t.Errorf("otrsAgentBase(%q) = %q, atteso %q", in, got, want)
		}
	}
}

func TestAdminTicketPage(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) {
		o.Tickets = &otrs.Mock{}
		o.Config.OTRS.URL = "https://otrs.example.it/otrs/nph-genericinterface.pl/Webservice/Prova"
		o.Config.OTRS.Queue = "Coda prova"
	})
	db.RecordTicket(database.TicketSent{Username: "mrossi", Name: "Mario Rossi", Email: "mrossi@example.it", Subject: "Stampante",
		TicketID: "42", TicketNumber: "2026100800000042", CustomerSet: false, Attachments: 2, CreatedAt: fixedNow})
	c := login(t, s)
	body := do(t, s, "GET", "/admin/ticket", nil, c, nil).Body.String()
	for _, want := range []string{"Coda prova", "Mario Rossi", "Stampante", "2026100800000042",
		"https://otrs.example.it/otrs/index.pl?Action=AgentTicketZoom;TicketID=42", "Cliente non impostato", `href="/admin/ticket" aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	if strings.Contains(body, "Password") {
		t.Error("credenziali nella pagina")
	}
	off, _ := newTestServer(t, nil)
	if b := do(t, off, "GET", "/admin/ticket", nil, login(t, off), nil).Body.String(); !strings.Contains(b, "Modulo spento") {
		t.Error("stato spento non mostrato")
	}
}
