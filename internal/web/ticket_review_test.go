package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

// blockingClient: Create resta in attesa finché il test non chiude release.
type blockingClient struct{ entered, release chan struct{} }

func (b *blockingClient) Create(context.Context, otrs.NewTicket) (otrs.Created, error) {
	b.entered <- struct{}{}
	<-b.release
	return otrs.Created{TicketID: "1", TicketNumber: "N1", CustomerSet: true}, nil
}

func sendAsync(s *Server, c *http.Cookie, form url.Values) chan map[string]any {
	out := make(chan map[string]any, 1)
	go func() {
		req := httptest.NewRequest("POST", "/ticket", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.AddCookie(c)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		var v map[string]any
		json.Unmarshal(rec.Body.Bytes(), &v)
		out <- v
	}()
	return out
}

// Due invii contemporanei dello stesso utente: il secondo non deve partire
// (il limite si conta sul registro, scritto solo dopo la risposta di OTRS).
func TestTicketConcurrentSendRejected(t *testing.T) {
	bc := &blockingClient{entered: make(chan struct{}, 4), release: make(chan struct{})}
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Directory = ticketDirectory; o.Tickets = bc })
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	first := sendAsync(s, c, validTicket())
	<-bc.entered
	second := sendAsync(s, c, validTicket())
	pending := false
	select {
	case v := <-second:
		if v["errore"] != "in_corso" {
			t.Errorf("secondo invio: %v", v)
		}
	case <-time.After(3 * time.Second):
		t.Error("secondo invio non rifiutato mentre il primo è in corso")
		pending = true
	}
	close(bc.release)
	if v := <-first; v["ok"] != true {
		t.Fatalf("primo invio: %v", v)
	}
	if pending {
		<-second
	}
}

// La somma dei tempi verso OTRS deve restare sotto il timeout del proxy (60 s).
func TestTicketSendDeadlineUnderProxyTimeout(t *testing.T) {
	if ticketSendTimeout >= 60*time.Second || otrs.CreateTimeout+otrs.UpdateTimeout > ticketSendTimeout {
		t.Fatalf("tempi: invio %v, create %v, update %v", ticketSendTimeout, otrs.CreateTimeout, otrs.UpdateTimeout)
	}
}

// Un utente non può occupare tutti i posti di caricamento.
func TestTicketUploadPerUserCap(t *testing.T) {
	s, c := ticketServer(t)
	for i := 0; i < ticketMaxPerUser; i++ {
		if out := mediaPost(t, s, c, "/ticket/allegati", "application/x-www-form-urlencoded", []byte("nome=a.png")); out["ok"] != true {
			t.Fatalf("upload %d: %v", i+1, out)
		}
	}
	if out := mediaPost(t, s, c, "/ticket/allegati", "application/x-www-form-urlencoded", []byte("nome=a.png")); out["ok"] != false {
		t.Fatalf("oltre il tetto per utente: %v", out)
	}
	other := viewerCookie(t, s, identity.User{Username: "gbianchi", Name: "Giulia Bianchi"})
	if out := mediaPost(t, s, other, "/ticket/allegati", "application/x-www-form-urlencoded", []byte("nome=a.png")); out["ok"] != true {
		t.Fatalf("un altro utente resta bloccato: %v", out)
	}
}

// Un pezzo che arriva lentamente non deve bloccare gli altri caricamenti.
func TestTicketSlowChunkDoesNotBlockOthers(t *testing.T) {
	s, c := ticketServer(t)
	id := mediaPost(t, s, c, "/ticket/allegati", "application/x-www-form-urlencoded", []byte("nome=a.png"))["id"].(string)
	pr, pw := io.Pipe()
	defer pw.Close()
	go func() {
		req := httptest.NewRequest("POST", "/ticket/allegati/"+id+"/pezzo?n=0", pr)
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.AddCookie(c)
		s.Handler().ServeHTTP(httptest.NewRecorder(), req)
	}()
	time.Sleep(100 * time.Millisecond) // il primo handler è fermo sulla lettura del corpo
	other := viewerCookie(t, s, identity.User{Username: "gbianchi", Name: "Giulia Bianchi"})
	done := make(chan map[string]any, 1)
	go func() {
		req := httptest.NewRequest("POST", "/ticket/allegati", strings.NewReader("nome=b.png"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.AddCookie(other)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		var v map[string]any
		json.Unmarshal(rec.Body.Bytes(), &v)
		done <- v
	}()
	select {
	case v := <-done:
		if v["ok"] != true {
			t.Fatalf("avvio: %v", v)
		}
	case <-time.After(3 * time.Second):
		pw.Close()
		<-done
		t.Fatal("un pezzo lento blocca gli altri caricamenti")
	}
}

// OTRS giù: gli allegati restano disponibili per riprovare con lo stesso dialog.
func TestTicketAttachmentsSurviveOTRSFailure(t *testing.T) {
	m := &otrs.Mock{Err: otrs.ErrOTRS}
	s, c := ticketTestServer(t, m)
	id := uploadTicketFile(t, s, c, "a.png", pngBytes)["id"].(string)
	f := validTicket()
	f.Add("allegato", id)
	if out := postTicket(t, s, c, f); out["errore"] != "otrs" {
		t.Fatalf("OTRS giù: %v", out)
	}
	m.Err = nil
	out := postTicket(t, s, c, f)
	if out["ok"] != true || len(m.Sent) != 1 || len(m.Sent[0].Attachments) != 1 {
		t.Fatalf("nuovo tentativo con lo stesso allegato: %v", out)
	}
	if _, _, err := s.takeTicketFiles("mrossi", []string{id}); err == nil {
		t.Fatal("allegato riusabile dopo un invio riuscito")
	}
}
