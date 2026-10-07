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

func TestCarouselExcerpt(t *testing.T) {
	s, db := newTestServer(t, nil)
	long := strings.Repeat("parola ", 60) // 420 caratteri
	id, _ := db.CreateAlert(database.Alert{Title: "Lungo", Body: "**Inizio** " + long, Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	db.CreateAlert(database.Alert{Title: "Breve", Body: "Solo *questo*.", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	img, _ := db.CreateAlert(database.Alert{Title: "Foto", Body: "Vedi ![schema](/uploads/guide/x.png)", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	body := do(t, s, "GET", "/partials/alerts", nil, nil, nil).Body.String()
	if strings.Contains(body, "**Inizio**") || strings.Contains(body, strings.Repeat("parola ", 40)) || strings.Contains(body, "<img") {
		t.Fatalf("carosello non ridotto a estratto:\n%s", body)
	}
	if !strings.Contains(body, `href="/avvisi/`+itoa(id)+`"`) || !strings.Contains(body, `href="/avvisi/`+itoa(img)+`"`) || strings.Count(body, "Leggi tutto") != 2 {
		t.Fatalf("Leggi tutto solo per gli avvisi lunghi o con immagini:\n%s", body)
	}
}

// Review I1: nell'estratto i link non sono cliccabili e gli a capo spariscono:
// serve "Leggi tutto" anche per gli avvisi brevi con link o su più righe.
func TestCarouselMoreForLinksAndLines(t *testing.T) {
	s, db := newTestServer(t, nil)
	link, _ := db.CreateAlert(database.Alert{Title: "Link", Body: "Scaricalo da https://intranet.local/x", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	md, _ := db.CreateAlert(database.Alert{Title: "Md", Body: "Vedi [il modulo](https://intranet.local/m)", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	lines, _ := db.CreateAlert(database.Alert{Title: "Righe", Body: "Prima riga\nseconda riga", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	db.CreateAlert(database.Alert{Title: "Semplice", Body: "Solo testo.", Level: "news", StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	body := do(t, s, "GET", "/partials/alerts", nil, nil, nil).Body.String()
	for _, id := range []int64{link, md, lines} {
		if !strings.Contains(body, `href="/avvisi/`+itoa(id)+`"`) {
			t.Errorf("avviso %d senza Leggi tutto", id)
		}
	}
	if n := strings.Count(body, "Leggi tutto"); n != 3 {
		t.Errorf("Leggi tutto: %d, attesi 3", n)
	}
}
