package otrs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

type call struct {
	Method, Path string
	Body         map[string]any
}

// fakeOTRS registra le chiamate; create/update decidono la risposta.
func fakeOTRS(t *testing.T, create, update http.HandlerFunc) (*HTTPClient, *[]call) {
	t.Helper()
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, call{r.Method, r.URL.Path, body})
		switch r.URL.Path {
		case "/ws/TicketCreate":
			create(w, r)
		case "/ws/TicketUpdate":
			update(w, r)
		default:
			http.Error(w, "<html>cortesia</html>", 500)
		}
	}))
	t.Cleanup(srv.Close)
	c := &HTTPClient{Config: config.OTRS{URL: srv.URL + "/ws", RouteCreate: "/TicketCreate", RouteUpdate: "/TicketUpdate",
		User: "agente", Password: "segreta-123", Queue: "Coda prova"}, HTTP: srv.Client()}
	return c, &calls
}

func jsonReply(v string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		io.WriteString(w, v)
	}
}

var okCreate = jsonReply(`{"ArticleID":"1","TicketNumber":"2026100800000011","TicketID":"42"}`)
var okUpdate = jsonReply(`{"TicketNumber":"2026100800000011","TicketID":"42"}`)

func sample() NewTicket {
	return NewTicket{Name: "Mario Rossi", Email: "mrossi@example.it", Phone: "731",
		Subject: "Stampante", Body: "Non stampa più.\nÈ urgente: perché?",
		Attachments: []Attachment{{Filename: "../../scheda.png", ContentType: "image/png", Content: []byte("PNGDATA")}}}
}

func TestCreateSendsExpectedRequests(t *testing.T) {
	c, calls := fakeOTRS(t, okCreate, okUpdate)
	got, err := c.Create(context.Background(), sample())
	if err != nil {
		t.Fatal(err)
	}
	if got != (Created{TicketID: "42", TicketNumber: "2026100800000011", CustomerSet: true}) {
		t.Fatalf("risultato: %+v", got)
	}
	if len(*calls) != 2 || (*calls)[0].Method != "POST" || (*calls)[1].Method != "PATCH" {
		t.Fatalf("chiamate: %+v", *calls)
	}
	cr := (*calls)[0].Body
	ticket := cr["Ticket"].(map[string]any)
	article := cr["Article"].(map[string]any)
	if cr["UserLogin"] != "agente" || cr["Password"] != "segreta-123" || ticket["Queue"] != "Coda prova" ||
		ticket["State"] != "new" || ticket["Priority"] != "3 normal" || ticket["CustomerUser"] != "mrossi@example.it" ||
		ticket["Title"] != "Stampante" {
		t.Fatalf("ticket: %v", cr)
	}
	if article["From"] != `"Mario Rossi" <mrossi@example.it>` || article["ArticleType"] != "webrequest" ||
		article["SenderType"] != "customer" || article["ContentType"] != "text/plain; charset=utf8" {
		t.Fatalf("articolo: %v", article)
	}
	if article["Body"] != "Non stampa più.\nÈ urgente: perché?\n\nTelefono / interno: 731" {
		t.Fatalf("corpo: %q", article["Body"])
	}
	att := cr["Attachment"].([]any)[0].(map[string]any)
	if att["Filename"] != "scheda.png" || att["ContentType"] != "image/png" || att["Content"] != base64.StdEncoding.EncodeToString([]byte("PNGDATA")) {
		t.Fatalf("allegato: %v", att)
	}
	up := (*calls)[1].Body
	upT := up["Ticket"].(map[string]any)
	if up["TicketID"] != "42" || upT["CustomerUser"] != "mrossi@example.it" || upT["CustomerID"] != "mrossi@example.it" {
		t.Fatalf("update: %v", up)
	}
}

func TestCreateWithoutPhoneOrAttachments(t *testing.T) {
	c, calls := fakeOTRS(t, okCreate, okUpdate)
	tk := sample()
	tk.Phone, tk.Attachments = "", nil
	if _, err := c.Create(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
	cr := (*calls)[0].Body
	if cr["Article"].(map[string]any)["Body"] != tk.Body {
		t.Fatalf("corpo senza telefono: %q", cr["Article"].(map[string]any)["Body"])
	}
	if _, ok := cr["Attachment"]; ok {
		t.Fatal("Attachment presente senza allegati")
	}
}

func TestCreateErrors(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"errore OTRS": jsonReply(`{"Error":{"ErrorCode":"TicketCreate.AuthFail","ErrorMessage":"no"}}`),
		"pagina HTML": func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "<html>cortesia</html>", 500) },
		"200 non JSON": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, "<html></html>")
		},
		"JSON senza numero": jsonReply(`{"TicketID":"42"}`),
	} {
		t.Run(name, func(t *testing.T) {
			c, calls := fakeOTRS(t, h, okUpdate)
			_, err := c.Create(context.Background(), sample())
			if !errors.Is(err, ErrOTRS) {
				t.Fatalf("atteso ErrOTRS, ottenuto %v", err)
			}
			if len(*calls) != 1 {
				t.Fatalf("dopo un create fallito nessun update: %d chiamate", len(*calls))
			}
		})
	}
}

func TestCreateUpdateFailureStillSucceeds(t *testing.T) {
	c, _ := fakeOTRS(t, okCreate, jsonReply(`{"Error":{"ErrorCode":"TicketUpdate.AccessDenied","ErrorMessage":"no"}}`))
	got, err := c.Create(context.Background(), sample())
	if err != nil || got.CustomerSet || got.TicketNumber != "2026100800000011" {
		t.Fatalf("update fallito: %+v %v", got, err)
	}
}

func TestCreateTimeoutAndRedirect(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(200 * time.Millisecond) }))
	defer slow.Close()
	c := &HTTPClient{Config: config.OTRS{URL: slow.URL, RouteCreate: "/c", RouteUpdate: "/u", User: "a", Password: "b", Queue: "q"},
		HTTP: &http.Client{Timeout: 50 * time.Millisecond}}
	if _, err := c.Create(context.Background(), sample()); !errors.Is(err, ErrOTRS) {
		t.Fatalf("timeout: %v", err)
	}
	redir := httptest.NewServer(http.RedirectHandler("https://altrove.example.it/", http.StatusFound))
	defer redir.Close()
	c2 := NewHTTPClient(config.OTRS{URL: redir.URL, RouteCreate: "/c", RouteUpdate: "/u", User: "a", Password: "b", Queue: "q"})
	if _, err := c2.Create(context.Background(), sample()); !errors.Is(err, ErrOTRS) {
		t.Fatalf("redirect seguito: %v", err)
	}
}

func TestPasswordNeverLogged(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	c, _ := fakeOTRS(t, jsonReply(`{"Error":{"ErrorCode":"TicketCreate.AuthFail","ErrorMessage":"no"}}`), okUpdate)
	_, err := c.Create(context.Background(), sample())
	if strings.Contains(buf.String(), "segreta-123") || strings.Contains(err.Error(), "segreta-123") || strings.Contains(err.Error(), "agente") {
		t.Fatalf("credenziali nei log o nell'errore: %s / %v", buf.String(), err)
	}
	if !strings.Contains(buf.String(), "TicketCreate.AuthFail") {
		t.Fatalf("ErrorCode non nel log: %s", buf.String())
	}
}

func TestMock(t *testing.T) {
	m := &Mock{}
	got, err := m.Create(context.Background(), sample())
	if err != nil || got.TicketNumber == "" || !got.CustomerSet || len(m.Sent) != 1 {
		t.Fatalf("mock: %+v %v", got, err)
	}
	m.Err = ErrOTRS
	if _, err := m.Create(context.Background(), sample()); !errors.Is(err, ErrOTRS) {
		t.Fatal("mock con errore")
	}
}

func TestSafeFilename(t *testing.T) {
	for in, want := range map[string]string{
		"../../scheda.png":                "scheda.png",
		`C:\Users\x\Desktop\a.pdf`:        "a.pdf",
		"":                                "allegato",
		strings.Repeat("a", 150) + ".png": strings.Repeat("a", 96) + ".png",
	} {
		if got := safeFilename(in); got != want {
			t.Errorf("safeFilename(%q) = %q, atteso %q", in, got, want)
		}
	}
}

// Un update lento non deve far superare il tempo totale: il ticket esiste già.
func TestUpdateTimeoutIndependent(t *testing.T) {
	slowUpdate := func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}
	c, _ := fakeOTRS(t, okCreate, slowUpdate)
	c.UpdateTimeout = 50 * time.Millisecond
	start := time.Now()
	got, err := c.Create(context.Background(), sample())
	if err != nil || got.CustomerSet || time.Since(start) > time.Second {
		t.Fatalf("update lento: %+v %v in %v", got, err, time.Since(start))
	}
}
