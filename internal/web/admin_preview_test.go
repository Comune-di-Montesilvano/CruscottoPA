package web

import (
	"net/url"
	"strings"
	"testing"
)

func TestAdminPreview(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	rec := do(t, s, "POST", "/admin/anteprima", url.Values{"body": {"**ciao** <script>x</script>"}}, c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<strong>ciao</strong>") || strings.Contains(rec.Body.String(), "<script") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, "POST", "/admin/anteprima", url.Values{"body": {"x"}}, nil, hx); strings.Contains(rec.Body.String(), "<p>x</p>") {
		t.Fatal("anteprima senza login")
	}
}
