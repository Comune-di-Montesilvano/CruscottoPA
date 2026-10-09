package web

import (
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

// Modulo spento: niente caricamenti (riempirebbero UPLOAD_DIR/.tmp senza scopo).
func TestTicketUploadDisabledModule(t *testing.T) {
	s, _ := newTestServer(t, nil) // Tickets nil
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	if out := uploadTicketFile(t, s, c, "a.png", pngBytes); out["ok"] != false {
		t.Fatalf("upload con modulo spento: %v", out)
	}
}

// Il nome del richiedente viene da AD (displayName), non dal cookie (fino a 30 giorni).
func TestTicketRequesterNameFromAD(t *testing.T) {
	m := otrs.NewMock()
	dir := ticketDirectory
	dir.profiles = map[string]audience.Profile{
		"mrossi": {Username: "mrossi", Attrs: map[string][]string{"mail": {"mrossi@example.it"}, "displayname": {"Mario Rossi Bianchi"}}},
	}
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Directory = dir; o.Tickets = m })
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	if out := postTicket(t, s, c, validTicket()); out["ok"] != true {
		t.Fatalf("invio: %v", out)
	}
	if m.Sent[0].Name != "Mario Rossi Bianchi" {
		t.Fatalf("nome: %q", m.Sent[0].Name)
	}
}

// Un caricamento più vecchio di un'ora non si completa né si usa.
func TestTicketUploadExpiresEverywhere(t *testing.T) {
	s, c := ticketTestServer(t, otrs.NewMock())
	start := mediaPost(t, s, c, "/ticket/allegati", "application/x-www-form-urlencoded", []byte("nome=a.png"))
	id := start["id"].(string)
	mediaPost(t, s, c, "/ticket/allegati/"+id+"/pezzo?n=0", "application/octet-stream", pngBytes)
	done := uploadTicketFile(t, s, c, "b.png", pngBytes)["id"].(string)
	later := fixedNow.Add(ticketUploadTTL + time.Minute)
	s.now = func() time.Time { return later }
	if out := mediaPost(t, s, c, "/ticket/allegati/"+id+"/fine", "application/x-www-form-urlencoded", nil); out["ok"] != false {
		t.Errorf("fine di un caricamento scaduto: %v", out)
	}
	if _, _, err := s.takeTicketFiles("mrossi", []string{done}); err == nil {
		t.Error("allegato scaduto usato in un invio")
	}
}

type recordingDirectory struct {
	fakeDirectory
	attrs *[]string
}

func (d recordingDirectory) Profile(u string, attrs []string) (audience.Profile, error) {
	*d.attrs = append([]string{}, attrs...)
	return d.fakeDirectory.Profile(u, attrs)
}

// Nessun attributo ripetuto nella richiesta ad AD.
func TestProfileAttributesDeduplicated(t *testing.T) {
	var got []string
	s, db := newTestServerWith(t, nil, func(o *Options) { o.Directory = recordingDirectory{fakeDirectory: ticketDirectory, attrs: &got} })
	db.CreateAudienceAttribute("Mail", "Posta")
	s.profileFor("mrossi")
	seen := map[string]bool{}
	for _, a := range got {
		k := strings.ToLower(a)
		if seen[k] {
			t.Fatalf("attributo ripetuto %q in %v", a, got)
		}
		seen[k] = true
	}
	if !seen["mail"] || !seen["telephonenumber"] || !seen["displayname"] {
		t.Fatalf("attributi richiesti: %v", got)
	}
}
