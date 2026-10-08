package web

import (
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

func mockConversation() *otrs.Mock {
	m := mockWithTickets()
	m.Tickets["5"].Articles = []otrs.Article{
		{ArticleID: "100", From: "Mario Rossi", Body: "Non stampa.\n<script>alert(1)</script>", Created: fixedNow.Add(-2 * time.Hour)},
		{ArticleID: "102", FromAgent: true, From: "Assistenza", Body: "Provi a riavviare.", Created: fixedNow.Add(-time.Hour),
			Attachments: []otrs.AttachmentInfo{{FileID: "1", Filename: "guida.pdf", ContentType: "application/pdf", Size: 9}}},
	}
	return m
}

func TestTicketPage(t *testing.T) {
	s, c := ticketTestServer(t, mockConversation())
	rec := do(t, s, "GET", "/ticket/5", nil, c, nil)
	body := rec.Body.String()
	for _, want := range []string{"Stampante ferma", "2026100800000005", "In lavorazione", "Provi a riavviare.",
		"/ticket/5/allegati/102/1", "guida.pdf", "data-ticket-reply", "&lt;script&gt;"} {
		if !strings.Contains(body, want) {
			t.Errorf("pagina: manca %q", want)
		}
	}
	if strings.Contains(body, "<script>alert") {
		t.Error("HTML degli articoli interpretato")
	}
	seen, _ := s.db.TicketSeen("mrossi")
	if _, ok := seen["5"]; !ok {
		t.Error("apertura della pagina non registrata come vista")
	}
}

func TestTicketPageNotAvailable(t *testing.T) {
	m := mockConversation()
	s, c := ticketTestServer(t, m)
	for _, path := range []string{"/ticket/9", "/ticket/999", "/ticket/abc"} {
		rec := do(t, s, "GET", path, nil, c, nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Ticket non disponibile") || strings.Contains(rec.Body.String(), "Di un altro") {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
	if rec := do(t, s, "GET", "/ticket/5", nil, nil, nil); !strings.Contains(rec.Body.String(), "Ticket non disponibile") {
		t.Error("anonimo: ticket mostrato")
	}
	m.Err = otrs.ErrOTRS
	s.ticketCache.forget("mrossi@example.it")
	if rec := do(t, s, "GET", "/ticket/5", nil, c, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Ticket non disponibile") {
		t.Errorf("OTRS giù: %d", rec.Code)
	}
}

func TestTicketAttachmentDownload(t *testing.T) {
	s, c := ticketTestServer(t, mockConversation())
	rec := do(t, s, "GET", "/ticket/5/allegati/102/1", nil, c, nil)
	if rec.Code != 200 || rec.Body.String() != "contenuto di guida.pdf" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "object-src 'self'") {
		t.Fatalf("PDF: %d %q %v", rec.Code, rec.Body, rec.Header())
	}
	other := viewerCookie(t, s, identity.User{Username: "gbianchi", Name: "Giulia Bianchi"})
	if rec := do(t, s, "GET", "/ticket/5/allegati/102/1", nil, other, nil); rec.Code == 200 && rec.Body.String() == "contenuto di guida.pdf" {
		t.Fatal("allegato servito a un altro utente")
	}
}

func TestAttachmentHeaders(t *testing.T) {
	for ct, want := range map[string]string{
		"image/png":       "sandbox",
		"application/pdf": "object-src 'self'",
		"text/html":       "attachment",
	} {
		h := attachmentHeaders(ct, "x")
		if !strings.Contains(h.Get("Content-Security-Policy")+h.Get("Content-Disposition"), want) {
			t.Errorf("%s: %v", ct, h)
		}
		if ct == "text/html" && h.Get("Content-Type") != "application/octet-stream" {
			t.Errorf("text/html servito come %q", h.Get("Content-Type"))
		}
	}
}
