package web

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestRefreshGuides(t *testing.T) {
	calls := 0
	s, db := newTestServerWith(t, nil, func(o *Options) {
		o.Config.GuideRefreshHours = 6
		o.GuideFetch = func(context.Context, string) (string, error) { calls++; return "# nuovo", nil }
	})
	stale, _ := db.CreateGuide(database.Guide{Title: "Vecchia", Kind: database.GuideKindGitHub, SourceURL: "https://github.com/o/r/blob/main/a.md", Body: "# vecchio", Enabled: true})
	db.SetGuideFetched(stale, "# vecchio", fixedNow.Add(-7*time.Hour))
	fresh, _ := db.CreateGuide(database.Guide{Title: "Fresca", Kind: database.GuideKindGitHub, SourceURL: "https://github.com/o/r/blob/main/b.md", Body: "# fresco", Enabled: true})
	db.SetGuideFetched(fresh, "# fresco", fixedNow.Add(-time.Hour))
	s.RefreshGuides(context.Background())
	if g, _ := db.GetGuide(stale); g.Body != "# nuovo" {
		t.Errorf("vecchia non aggiornata: %q", g.Body)
	}
	if g, _ := db.GetGuide(fresh); g.Body != "# fresco" || calls != 1 {
		t.Errorf("fresca toccata: %q, chiamate %d", g.Body, calls)
	}
}

func TestRefreshKeepsBodyOnError(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) {
		o.Config.GuideRefreshHours = 6
		o.GuideFetch = func(context.Context, string) (string, error) { return "", errors.New("timeout") }
	})
	id, _ := db.CreateGuide(database.Guide{Title: "G", Kind: database.GuideKindGitHub, SourceURL: "https://github.com/o/r/blob/main/a.md", Body: "# buona", Enabled: true})
	s.RefreshGuides(context.Background())
	if g, _ := db.GetGuide(id); g.Body != "# buona" || g.FetchError == "" {
		t.Fatalf("%+v", g)
	}
}

func TestRefreshGuidesOff(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) {
		o.GuideFetch = func(context.Context, string) (string, error) {
			t.Fatal("refresh con GUIDE_REFRESH_HOURS=0")
			return "", nil
		}
	})
	db.CreateGuide(database.Guide{Title: "G", Kind: database.GuideKindGitHub, SourceURL: "https://github.com/o/r/blob/main/a.md", Enabled: true})
	s.RefreshGuides(context.Background())
}
