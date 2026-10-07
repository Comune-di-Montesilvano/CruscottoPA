package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

var hx = map[string]string{"HX-Request": "true"}

func TestCategoriesPageAndCreate(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)

	rec := do(t, s, "GET", "/admin/categorie", nil, c, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `id="section"`) || !strings.Contains(rec.Body.String(), "Applicativi") {
		t.Fatalf("pagina categorie: %d\n%s", rec.Code, rec.Body)
	}

	rec = do(t, s, "POST", "/admin/categorie", url.Values{"name": {"  Gestionali esterni "}}, c, hx)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "<html") || !strings.Contains(rec.Body.String(), "Gestionali esterni") {
		t.Fatalf("create: atteso frammento con la nuova categoria, %d\n%s", rec.Code, rec.Body)
	}
	cats, _ := db.ListCategories()
	if len(cats) != 2 || cats[1].Name != "Gestionali esterni" {
		t.Fatalf("DB: %+v", cats)
	}
}

func TestCategoryValidationAndDuplicates(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)

	rec := do(t, s, "POST", "/admin/categorie", url.Values{"name": {"   "}}, c, hx)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Campo obbligatorio.") {
		t.Fatalf("nome vuoto: %d\n%s", rec.Code, rec.Body)
	}
	rec = do(t, s, "POST", "/admin/categorie", url.Values{"name": {"applicativi"}}, c, hx)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Esiste già") {
		t.Fatalf("duplicato: %d\n%s", rec.Code, rec.Body)
	}
}

func TestCategoryEditUpdateMoveDelete(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	id, _ := db.CreateCategory("Esterni")
	path := "/admin/categorie/" + itoa(id)

	rec := do(t, s, "GET", path+"/modifica", nil, c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `value="Esterni"`) || !strings.Contains(rec.Body.String(), `hx-post="`+path+`"`) {
		t.Fatalf("modifica: %d\n%s", rec.Code, rec.Body)
	}
	if rec := do(t, s, "POST", path, url.Values{"name": {"Gestionali esterni"}}, c, hx); rec.Code != 200 {
		t.Fatalf("update: %d", rec.Code)
	}
	if rec := do(t, s, "POST", path+"/sposta", url.Values{"dir": {"up"}}, c, hx); rec.Code != 200 {
		t.Fatalf("sposta: %d", rec.Code)
	}
	cats, _ := db.ListCategories()
	if cats[0].Name != "Gestionali esterni" {
		t.Fatalf("dopo update+sposta: %+v", cats)
	}
	if rec := do(t, s, "POST", path+"/elimina", nil, c, hx); rec.Code != 200 {
		t.Fatalf("elimina: %d", rec.Code)
	}
	if rec := do(t, s, "POST", "/admin/categorie/999/elimina", nil, c, hx); rec.Code != http.StatusNotFound {
		t.Fatalf("elimina inesistente: %d", rec.Code)
	}
	if rec := do(t, s, "GET", "/admin/categorie/abc/modifica", nil, c, hx); rec.Code != http.StatusNotFound {
		t.Fatalf("id non numerico: %d", rec.Code)
	}
}

func TestDeleteCategoryWithAppsShowsMessage(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	cats, _ := db.ListCategories()
	rec := do(t, s, "POST", "/admin/categorie/"+itoa(cats[0].ID)+"/elimina", nil, c, hx)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Sposta o elimina prima le app") {
		t.Fatalf("categoria con app: %d\n%s", rec.Code, rec.Body)
	}
}

func TestCategoriesRequireLogin(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if rec := do(t, s, "POST", "/admin/categorie", url.Values{"name": {"x"}}, nil, hx); rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/admin/login" {
		t.Fatalf("POST senza sessione: %d", rec.Code)
	}
}
