package web

import (
	"net/url"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

func TestPresenceFromDashboard(t *testing.T) {
	s, db := newTestServer(t, nil)
	mario := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	do(t, s, "GET", "/", nil, mario, nil)
	if rec := do(t, s, "POST", "/presenza", url.Values{"app": {"1"}, "permesso": {"granted"}}, mario, nil); rec.Code != 200 {
		t.Fatalf("/presenza: %d", rec.Code)
	}
	ps, _ := db.ListPresence()
	if len(ps) != 1 || ps[0].Username != "mrossi" || ps[0].Name != "Mario Rossi" || ps[0].LastApp == nil || ps[0].Permission != "granted" {
		t.Fatalf("presenza: %+v", ps)
	}
	// Anonimo e admin senza cookie utente: nessuna presenza.
	do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Anonymous: true}), nil)
	do(t, s, "GET", "/", nil, login(t, s), nil)
	if rec := do(t, s, "POST", "/presenza", url.Values{"app": {"1"}}, nil, nil); rec.Code != 200 {
		t.Fatalf("/presenza anonima: %d", rec.Code)
	}
	if ps, _ = db.ListPresence(); len(ps) != 1 {
		t.Fatalf("presenze in più: %+v", ps)
	}
}

func TestReadFromPlancia(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db) // "Urgente riservato" solo al gruppo Nessuno; "Novità per tutti" pubblica
	alerts, _ := db.ListActiveAlerts(fixedNow)
	var public, reserved int64
	for _, a := range alerts {
		switch a.Title {
		case "Novità per tutti":
			public = a.ID
		case "Urgente riservato":
			reserved = a.ID
		}
	}
	mario := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	if rec := do(t, s, "POST", "/avvisi/"+itoa(public)+"/letto", url.Values{"come": {"conferma"}}, mario, nil); rec.Code != 200 {
		t.Fatalf("letto: %d", rec.Code)
	}
	do(t, s, "POST", "/avvisi/"+itoa(reserved)+"/letto", url.Values{"come": {"conferma"}}, mario, nil) // non visibile a lui
	do(t, s, "GET", "/avvisi/"+itoa(reserved), nil, mario, nil)                                       // idem, dalla pagina
	if rec := do(t, s, "POST", "/avvisi/abc/letto", nil, mario, nil); rec.Code != 200 {               // id non valido
		t.Fatalf("id non valido: %d", rec.Code)
	}
	do(t, s, "POST", "/avvisi/99999/letto", url.Values{"come": {"conferma"}}, mario, nil) // inesistente
	if rs, _ := db.ReadsFor(public); len(rs) != 1 || rs[0].How != database.ReadConfirm {
		t.Fatalf("letture pubblico: %+v", rs)
	}
	if rs, _ := db.ReadsFor(reserved); len(rs) != 0 {
		t.Fatalf("lettura di un avviso non visibile: %+v", rs)
	}
	// La pagina dell'avviso conta come apertura.
	anna := viewerCookie(t, s, identity.User{Username: "senzanome"})
	do(t, s, "GET", "/avvisi/"+itoa(public), nil, anna, nil)
	if rs, _ := db.ReadsFor(public); len(rs) != 2 || rs[1].Username != "senzanome" || rs[1].How != database.ReadOpen {
		t.Fatalf("apertura dalla pagina: %+v", rs)
	}
}
