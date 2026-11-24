package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/rubrica"
)

type rubricaReply struct {
	OK      bool             `json:"ok"`
	Persone []rubrica.Person `json:"persone"`
}

func rubricaServer(t *testing.T, h http.HandlerFunc) (*Server, string) {
	t.Helper()
	fake := httptest.NewServer(h)
	t.Cleanup(fake.Close)
	s, _ := newTestServerWith(t, nil, func(o *Options) {
		o.Config.RubricaURL = fake.URL
		o.Rubrica = rubrica.New(fake.URL)
	})
	return s, fake.URL
}

func getRubrica(t *testing.T, s *Server, q string) rubricaReply {
	t.Helper()
	rec := do(t, s, "GET", "/rubrica/cerca?q="+q, nil, nil, nil)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status %d, %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var r rubricaReply
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("%v\n%s", err, rec.Body)
	}
	if r.Persone == nil {
		t.Fatalf("persone null: %s", rec.Body)
	}
	return r
}

func TestRubricaRouteOK(t *testing.T) {
	s, base := rubricaServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"uid":"mrossi","nome":"Mario Rossi","reparto":"Tributi","interno":"123","mail":"mrossi@example.it"}]`))
	})
	r := getRubrica(t, s, "rossi")
	if !r.OK || len(r.Persone) != 1 || r.Persone[0].URL != base+"/contacts/mrossi" || r.Persone[0].Interno != "123" {
		t.Fatalf("%+v", r)
	}
}

func TestRubricaRouteDown(t *testing.T) {
	s, _ := rubricaServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	if r := getRubrica(t, s, "rossi"); r.OK || len(r.Persone) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestRubricaRouteOff(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if r := getRubrica(t, s, "rossi"); r.OK || len(r.Persone) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestRubricaDataAttr(t *testing.T) {
	s, _ := rubricaServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) })
	if body := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(body, `data-rubrica="1"`) {
		t.Fatal("manca data-rubrica con RUBRICA_URL")
	}
	off, _ := newTestServer(t, nil)
	if body := do(t, off, "GET", "/", nil, nil, nil).Body.String(); strings.Contains(body, "data-rubrica") {
		t.Fatal("data-rubrica senza RUBRICA_URL")
	}
}

func TestDashboardJSRubrica(t *testing.T) {
	js, err := os.ReadFile("../../web/static/js/dashboard.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"/rubrica/cerca?q="`, "AbortController", `"Persone"`, `"person"`} {
		if !strings.Contains(string(js), want) {
			t.Errorf("dashboard.js: manca %s", want)
		}
	}
}
