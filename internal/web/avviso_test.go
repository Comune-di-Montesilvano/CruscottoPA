package web

import (
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestAlertPage(t *testing.T) {
	s, db := newTestServer(t, nil)
	id, _ := db.CreateAlert(database.Alert{Title: "Sciopero", Body: "## Dettagli\n\nUffici **chiusi**.", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	rec := do(t, s, "GET", "/avvisi/"+itoa(id), nil, nil, nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "<h3") || !strings.Contains(body, "<strong>chiusi</strong>") || !strings.Contains(body, "Sciopero") {
		t.Fatalf("pagina avviso: %d\n%s", rec.Code, body)
	}
	future, _ := db.CreateAlert(database.Alert{Title: "Futuro", Level: "news", StartsAt: fixedNow.Add(time.Hour), CreatedAt: fixedNow})
	for _, target := range []string{"/avvisi/" + itoa(future), "/avvisi/9999", "/avvisi/abc"} {
		rec := do(t, s, "GET", target, nil, nil, nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Avviso non disponibile") || strings.Contains(rec.Body.String(), "Futuro") {
			t.Errorf("%s: %d", target, rec.Code)
		}
	}
}

func TestAvvisiPageRendersMarkdown(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.CreateAlert(database.Alert{Title: "A", Body: "- uno\n- due", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	if body := do(t, s, "GET", "/avvisi", nil, nil, nil).Body.String(); !strings.Contains(body, "<li>uno</li>") {
		t.Fatalf("/avvisi senza Markdown:\n%s", body)
	}
}

func TestCarouselExpandable(t *testing.T) {
	s, db := newTestServer(t, nil)
	long := strings.Repeat("parola ", 60) // 420 caratteri
	db.CreateAlert(database.Alert{Title: "Lungo", Body: "**Inizio** " + long, Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	db.CreateAlert(database.Alert{Title: "Breve", Body: "Solo *questo*.", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	db.CreateAlert(database.Alert{Title: "Foto", Body: "Vedi ![schema](/uploads/guide/x.png)", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	body := do(t, s, "GET", "/partials/alerts", nil, nil, nil).Body.String()
	if strings.Contains(body, "**Inizio**") || strings.Contains(body, "Leggi tutto") || strings.Contains(body, `href="/avvisi/`) {
		t.Fatalf("niente link per il singolo avviso, solo l'estratto e l'espansione:\n%s", body)
	}
	if n := strings.Count(body, `<details class="news-expand"`); n != 2 {
		t.Fatalf("espansione solo per gli avvisi lunghi o con immagini: %d\n%s", n, body)
	}
	// Il testo completo è nella card, nascosto finché non si espande.
	full := body[strings.Index(body, `<details class="news-expand"`):]
	if !strings.Contains(full, "<strong>Inizio</strong>") || !strings.Contains(body, `<img src="/uploads/guide/x.png"`) {
		t.Fatalf("testo completo nell'espansione:\n%s", body)
	}
	if !strings.Contains(body, `href="/avvisi">Tutti gli avvisi`) {
		t.Fatal("manca Tutti gli avvisi")
	}
}

// Review I1: nell'estratto i link non sono cliccabili e gli a capo spariscono:
// serve l'espansione anche per gli avvisi brevi con link o su più righe.
func TestCarouselMoreForLinksAndLines(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.CreateAlert(database.Alert{Title: "Link", Body: "Scaricalo da https://intranet.local/x", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	db.CreateAlert(database.Alert{Title: "Md", Body: "Vedi [il modulo](https://intranet.local/m)", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	db.CreateAlert(database.Alert{Title: "Righe", Body: "Prima riga\nseconda riga", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	db.CreateAlert(database.Alert{Title: "Semplice", Body: "Solo testo.", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	body := do(t, s, "GET", "/partials/alerts", nil, nil, nil).Body.String()
	if n := strings.Count(body, `<details class="news-expand"`); n != 3 {
		t.Errorf("espansioni: %d, attese 3", n)
	}
	if !strings.Contains(body, `href="https://intranet.local/m"`) {
		t.Error("nel testo espanso i link devono essere cliccabili")
	}
}
