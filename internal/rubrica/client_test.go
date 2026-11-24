package rubrica

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRubrica risponde a /api/cerca con h e conta le richieste.
func fakeRubrica(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if r.URL.Path != "/api/cerca" {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func jsonPeople(w http.ResponseWriter, people ...map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	if people == nil {
		people = []map[string]string{}
	}
	json.NewEncoder(w).Encode(people)
}

func TestNewEmpty(t *testing.T) {
	if New("") != nil {
		t.Fatal("base vuota: atteso nil")
	}
}

func TestRubricaSearchOK(t *testing.T) {
	srv, _ := fakeRubrica(t, func(w http.ResponseWriter, r *http.Request) {
		jsonPeople(w, map[string]string{"uid": "mrossi", "nome": "Mario Rossi", "reparto": "Tributi", "interno": "123", "mail": "mrossi@example.it"})
	})
	got, err := New(srv.URL).Search(context.Background(), "rossi")
	want := Person{UID: "mrossi", Nome: "Mario Rossi", Reparto: "Tributi", Interno: "123", Mail: "mrossi@example.it", URL: srv.URL + "/contacts/mrossi"}
	if err != nil || len(got) != 1 || got[0] != want {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestRubricaQueryEncoding(t *testing.T) {
	var q atomic.Value
	srv, _ := fakeRubrica(t, func(w http.ResponseWriter, r *http.Request) {
		q.Store(r.URL.Query().Get("q"))
		jsonPeople(w)
	})
	if _, err := New(srv.URL).Search(context.Background(), "D'Angelo Niccolò"); err != nil {
		t.Fatal(err)
	}
	if got := q.Load(); got != "D'Angelo Niccolò" {
		t.Fatalf("q = %q", got)
	}
}

func TestRubricaUIDEscaped(t *testing.T) {
	srv, _ := fakeRubrica(t, func(w http.ResponseWriter, r *http.Request) {
		jsonPeople(w, map[string]string{"uid": "a/b c", "nome": "X"})
	})
	got, err := New(srv.URL).Search(context.Background(), "xx")
	if err != nil || len(got) != 1 || got[0].URL != srv.URL+"/contacts/a%2Fb%20c" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestRubricaErrors(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"500":       func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
		"html":      func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>pagina di cortesia</html>")) },
		"redirect":  func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/login", http.StatusFound) },
		"grande":    func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("[" + strings.Repeat(" ", 300<<10) + "]")) },
		"lenta":     func(w http.ResponseWriter, r *http.Request) { time.Sleep(3 * time.Second); jsonPeople(w) },
		"non array": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"errore":"x"}`)) },
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			srv, _ := fakeRubrica(t, h)
			start := time.Now()
			got, err := New(srv.URL).Search(context.Background(), "rossi")
			if err == nil || len(got) != 0 {
				t.Fatalf("atteso errore: %+v %v", got, err)
			}
			if d := time.Since(start); d > 2500*time.Millisecond {
				t.Fatalf("troppo lento: %v", d)
			}
		})
	}
}

func TestRubricaMaxResults(t *testing.T) {
	srv, _ := fakeRubrica(t, func(w http.ResponseWriter, r *http.Request) {
		var ps []map[string]string
		for i := 0; i < 12; i++ {
			ps = append(ps, map[string]string{"uid": fmt.Sprint(i), "nome": "P"})
		}
		jsonPeople(w, ps...)
	})
	got, err := New(srv.URL).Search(context.Background(), "pp")
	if err != nil || len(got) != 8 {
		t.Fatalf("%d %v", len(got), err)
	}
}

func TestRubricaCache(t *testing.T) {
	srv, n := fakeRubrica(t, func(w http.ResponseWriter, r *http.Request) {
		jsonPeople(w, map[string]string{"uid": "mrossi", "nome": "Mario Rossi"})
	})
	c := New(srv.URL)
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	for _, q := range []string{"Mario Rossi", "  mario   ROSSI "} {
		if got, err := c.Search(context.Background(), q); err != nil || len(got) != 1 {
			t.Fatalf("%q: %+v %v", q, got, err)
		}
	}
	if n.Load() != 1 {
		t.Fatalf("richieste: %d, attesa 1", n.Load())
	}
	now = now.Add(2*time.Minute + time.Second)
	c.Search(context.Background(), "mario rossi")
	if n.Load() != 2 {
		t.Fatalf("dopo la scadenza: %d richieste, attese 2", n.Load())
	}
}

func TestRubricaErrorNotCached(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	srv, n := fakeRubrica(t, func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(500)
			return
		}
		jsonPeople(w)
	})
	c := New(srv.URL)
	c.Search(context.Background(), "rossi")
	fail.Store(false)
	if _, err := c.Search(context.Background(), "rossi"); err != nil || n.Load() != 2 {
		t.Fatalf("errore messo in cache: %v, %d richieste", err, n.Load())
	}
}

func TestRubricaShortQuery(t *testing.T) {
	srv, n := fakeRubrica(t, func(w http.ResponseWriter, r *http.Request) { jsonPeople(w) })
	got, err := New(srv.URL).Search(context.Background(), " è ")
	if err != nil || len(got) != 0 || n.Load() != 0 {
		t.Fatalf("%+v %v, %d richieste", got, err, n.Load())
	}
}
