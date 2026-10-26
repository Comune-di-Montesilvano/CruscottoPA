package web

import (
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestGuidePageMarkdownAndGitHub(t *testing.T) {
	s, db := newTestServer(t, nil)
	md, _ := db.CreateGuide(database.Guide{Title: "Interna", Kind: database.GuideKindMarkdown, Body: "# Passi\n\n1. uno", Enabled: true})
	gh, _ := db.CreateGuide(database.Guide{Title: "Repo", Kind: database.GuideKindGitHub, SourceURL: "https://github.com/o/r/blob/main/docs/a.md", Body: "![s](img/s.png) [altro](b.md)", Enabled: true})
	db.SetGuideFetched(gh, "https://github.com/o/r/blob/main/docs/a.md", "![s](img/s.png) [altro](b.md)", fixedNow)
	body := do(t, s, "GET", "/guide/"+itoa(md), nil, nil, nil).Body.String()
	if !strings.Contains(body, "<h2") || !strings.Contains(body, "<ol>") || !strings.Contains(body, "Interna") {
		t.Fatalf("markdown:\n%s", body)
	}
	body = do(t, s, "GET", "/guide/"+itoa(gh), nil, nil, nil).Body.String()
	for _, want := range []string{
		`src="https://raw.githubusercontent.com/o/r/main/docs/img/s.png"`,
		`href="https://github.com/o/r/blob/main/docs/b.md"`,
		`href="https://github.com/o/r/blob/main/docs/a.md"`, // fonte
	} {
		if !strings.Contains(body, want) {
			t.Errorf("github: manca %s", want)
		}
	}
}

func TestGuidePageVisibility(t *testing.T) {
	s, db := newTestServer(t, nil)
	apps, _ := db.ListApps() // seed: app senza URL → non visibili in plancia
	hidden := apps[0].ID
	off, _ := db.CreateGuide(database.Guide{Title: "Off", Kind: database.GuideKindMarkdown, Body: "segreto"})
	ofHidden, _ := db.CreateGuide(database.Guide{AppID: &hidden, Title: "H", Kind: database.GuideKindMarkdown, Body: "segreto", Enabled: true})
	restricted, _ := db.CreateGuide(database.Guide{Title: "Riservata", Kind: database.GuideKindMarkdown, Body: "segreto", Enabled: true})
	grp, _ := db.CreateAudienceGroup("Nessuno")
	db.SetContentAudience(database.ContentGuide, restricted, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{grp}})
	for _, id := range []int64{off, ofHidden, restricted, 9999} {
		for _, suffix := range []string{"", "/pdf"} {
			rec := do(t, s, "GET", "/guide/"+itoa(id)+suffix, nil, nil, nil)
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Guida non disponibile") || strings.Contains(rec.Body.String(), "segreto") {
				t.Errorf("guida %d%s: %d", id, suffix, rec.Code)
			}
		}
	}
	if rec := do(t, s, "GET", "/guide/abc", nil, nil, nil); !strings.Contains(rec.Body.String(), "Guida non disponibile") {
		t.Error("id non numerico")
	}
}

func TestGuidePDF(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	name := uploadMedia(t, s, c, "pdf", pdfBytes)["name"].(string)
	id, _ := db.CreateGuide(database.Guide{Title: "Manuale d'uso", Kind: database.GuideKindPDF, File: name, Enabled: true})
	rec := do(t, s, "GET", "/guide/"+itoa(id)+"/pdf", nil, nil, nil)
	h := rec.Header()
	if rec.Code != 200 || h.Get("Content-Type") != "application/pdf" || h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Content-Disposition") != `inline; filename="Manuale-d-uso.pdf"; filename*=UTF-8''Manuale-d-uso.pdf` || h.Get("Content-Security-Policy") != pdfCSP || rec.Body.String() != string(pdfBytes) {
		t.Fatalf("%d %v", rec.Code, h)
	}
	// /guide/{id} di un PDF porta al file.
	if rec := do(t, s, "GET", "/guide/"+itoa(id), nil, nil, nil); rec.Code != 303 || rec.Header().Get("Location") != "/guide/"+itoa(id)+"/pdf" {
		t.Fatalf("redirect: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestDashboardGuideLinks(t *testing.T) {
	s, db := newTestServer(t, nil)
	md, _ := db.CreateGuide(database.Guide{Title: "Interna", Kind: database.GuideKindMarkdown, Body: "x", Enabled: true})
	pdf, _ := db.CreateGuide(database.Guide{Title: "Manuale", Kind: database.GuideKindPDF, File: "x.pdf", Enabled: true})
	db.CreateGuide(database.Guide{Title: "Esterna", Kind: database.GuideKindLink, URL: "https://wiki.local/x", Enabled: true})
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	for _, want := range []string{
		`<a href="/guide/` + itoa(md) + `">Interna</a>`,
		`<a href="/guide/` + itoa(pdf) + `/pdf" target="_blank" rel="noopener">Manuale</a>`,
		`<a href="https://wiki.local/x" target="_blank" rel="noopener">Esterna</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %s", want)
		}
	}
}

// Il nome del PDF tiene le lettere accentate (filename*), con un ripiego ASCII
// senza accenti per i client che non lo capiscono.
func TestPDFFilename(t *testing.T) {
	for title, want := range map[string]string{
		"Manuale d'uso":  `filename="Manuale-d-uso.pdf"; filename*=UTF-8''Manuale-d-uso.pdf`,
		"Attività è già": `filename="Attivita-e-gia.pdf"; filename*=UTF-8''Attivit%C3%A0-%C3%A8-gi%C3%A0.pdf`,
		"Perché? Così!":  `filename="Perche-Cosi.pdf"; filename*=UTF-8''Perch%C3%A9-Cos%C3%AC.pdf`,
		"???":            `filename="guida.pdf"; filename*=UTF-8''guida.pdf`,
	} {
		if got := pdfDisposition(title); got != "inline; "+want {
			t.Errorf("%q: %s", title, got)
		}
	}
}

// Anteprima dall'admin: anche guide disattivate, riservate o di app nascoste.
// Protetta dalla sessione admin, non dall'identità dichiarata della plancia.
func TestAdminGuidePreview(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	off, _ := db.CreateGuide(database.Guide{Title: "Off", Kind: database.GuideKindMarkdown, Body: "segreto"})
	name := uploadMedia(t, s, c, "pdf", pdfBytes)["name"].(string)
	pdf, _ := db.CreateGuide(database.Guide{Title: "Manuale", Kind: database.GuideKindPDF, File: name})
	if rec := do(t, s, "GET", "/admin/guide/"+itoa(off)+"/anteprima", nil, nil, nil); rec.Code != 303 || strings.Contains(rec.Body.String(), "segreto") {
		t.Fatalf("senza sessione: %d", rec.Code)
	}
	if body := do(t, s, "GET", "/admin/guide/"+itoa(off)+"/anteprima", nil, c, nil).Body.String(); !strings.Contains(body, "segreto") {
		t.Fatalf("anteprima di una guida disattivata:\n%s", body)
	}
	if rec := do(t, s, "GET", "/admin/guide/"+itoa(pdf)+"/pdf", nil, c, nil); rec.Code != 200 || rec.Body.String() != string(pdfBytes) || rec.Header().Get("Content-Security-Policy") != pdfCSP {
		t.Fatalf("PDF di una guida disattivata: %d %v", rec.Code, rec.Header())
	}
	if rec := do(t, s, "GET", "/admin/guide/"+itoa(pdf)+"/anteprima", nil, c, nil); rec.Code != 303 || rec.Header().Get("Location") != "/admin/guide/"+itoa(pdf)+"/pdf" {
		t.Fatalf("anteprima di un PDF: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if body := do(t, s, "GET", "/admin/guide/9999/anteprima", nil, c, nil).Body.String(); !strings.Contains(body, "Guida non disponibile") {
		t.Fatal("guida inesistente")
	}
	body := do(t, s, "GET", "/admin/guide", nil, c, nil).Body.String()
	for _, want := range []string{`href="/admin/guide/` + itoa(off) + `/anteprima"`, `href="/admin/guide/` + itoa(pdf) + `/pdf"`} {
		if !strings.Contains(body, want) {
			t.Errorf("elenco dell'admin: manca %s", want)
		}
	}
}
