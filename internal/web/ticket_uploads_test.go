package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// uploadTicketFile carica data a pezzi come ticket.js; risposta finale o primo errore.
func uploadTicketFile(t *testing.T, s *Server, c *http.Cookie, name string, data []byte) map[string]any {
	t.Helper()
	start := mediaPost(t, s, c, "/ticket/allegati", "application/x-www-form-urlencoded", []byte("nome="+name))
	if start["ok"] != true {
		return start
	}
	id := start["id"].(string)
	chunk := int(start["chunk"].(float64))
	for n, off := 0, 0; off < len(data); n, off = n+1, off+chunk {
		end := min(off+chunk, len(data))
		r := mediaPost(t, s, c, "/ticket/allegati/"+id+"/pezzo?n="+itoa(int64(n)), "application/octet-stream", data[off:end])
		if r["ok"] != true {
			return r
		}
	}
	return mediaPost(t, s, c, "/ticket/allegati/"+id+"/fine", "application/x-www-form-urlencoded", nil)
}

func ticketServer(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	s, _ := newTestServer(t, nil)
	return s, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
}

func TestTicketUploadOK(t *testing.T) {
	s, c := ticketServer(t)
	big := append(append([]byte{}, pngBytes...), make([]byte, 700<<10)...)
	out := uploadTicketFile(t, s, c, "schermata.png", big)
	if out["ok"] != true || out["nome"] != "schermata.png" || out["id"] == "" {
		t.Fatalf("upload: %v", out)
	}
	atts, paths, err := s.takeTicketFiles("mrossi", []string{out["id"].(string)})
	if err != nil || len(atts) != 1 || atts[0].ContentType != "image/png" || len(atts[0].Content) != len(big) {
		t.Fatalf("take: %v %d", err, len(atts))
	}
	if _, _, err := s.takeTicketFiles("mrossi", []string{out["id"].(string)}); err == nil {
		t.Fatal("id riusato: deve fallire")
	}
	s.removeTicketFiles(paths)
	if entries, _ := os.ReadDir(filepath.Join(s.cfg.UploadDir, ".tmp", "ticket")); len(entries) != 0 {
		t.Errorf("temporanei rimasti: %d", len(entries))
	}
}

func TestTicketUploadRejects(t *testing.T) {
	s, c := ticketServer(t)
	if out := uploadTicketFile(t, s, nil, "a.png", pngBytes); out["ok"] != false {
		t.Errorf("anonimo: %v", out)
	}
	anon := viewerCookie(t, s, identity.User{Anonymous: true})
	if out := uploadTicketFile(t, s, anon, "a.png", pngBytes); out["ok"] != false {
		t.Errorf("cookie anonimo: %v", out)
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	if out := uploadTicketFile(t, s, c, "a.svg", svg); out["ok"] != false {
		t.Errorf("SVG: %v", out)
	}
	if out := uploadTicketFile(t, s, c, "big.png", append(append([]byte{}, pngBytes...), make([]byte, 5<<20)...)); out["ok"] != false {
		t.Errorf("oltre 5 MB: %v", out)
	}
	if out := uploadTicketFile(t, s, c, "doc.pdf", pdfBytes); out["ok"] != true {
		t.Errorf("PDF valido: %v", out)
	}
}

func TestTicketFilesOtherUserAndLimits(t *testing.T) {
	s, c := ticketServer(t)
	other := viewerCookie(t, s, identity.User{Username: "gbianchi", Name: "Giulia Bianchi"})
	mine := uploadTicketFile(t, s, c, "a.png", pngBytes)["id"].(string)
	theirs := uploadTicketFile(t, s, other, "b.png", pngBytes)["id"].(string)
	if _, _, err := s.takeTicketFiles("mrossi", []string{theirs}); err == nil {
		t.Fatal("allegato di un altro utente accettato")
	}
	var ids []string
	for i := 0; i < 4; i++ {
		ids = append(ids, uploadTicketFile(t, s, c, "x.png", pngBytes)["id"].(string))
	}
	if _, _, err := s.takeTicketFiles("mrossi", ids); err == nil {
		t.Fatal("più di 3 allegati accettati")
	}
	if _, _, err := s.takeTicketFiles("mrossi", []string{mine, "nonesiste"}); err == nil {
		t.Fatal("id inesistente accettato")
	}
	s.consumeTicketFiles(append(ids, mine)) // tetto per utente: libera i posti
	// total: 3 file da 4 MB = 12 MB > 10 MB
	four := append(append([]byte{}, pngBytes...), make([]byte, 4<<20)...)
	var big []string
	for i := 0; i < 3; i++ {
		big = append(big, uploadTicketFile(t, s, c, "g.png", four)["id"].(string))
	}
	if _, _, err := s.takeTicketFiles("mrossi", big); err == nil {
		t.Fatal("oltre 10 MB totali accettati")
	}
}
