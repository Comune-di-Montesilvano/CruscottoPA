package backup

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// touch crea un archivio finto (Prune guarda solo i nomi).
func touch(t *testing.T, e *env, at time.Time, kind string) string {
	t.Helper()
	name := "cruscotto-" + at.Format(nameTimeLayout) + "-" + kind + ".tar.gz"
	if err := os.WriteFile(filepath.Join(e.s.dir, name), []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestPruneGFS(t *testing.T) {
	e := newEnv(t) // ora: 2026-10-06 08:00 UTC
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 3, 0, 0, 0, time.UTC) }

	keep := []string{
		touch(t, e, d(2026, 10, 5), KindAuto),      // giornaliero
		touch(t, e, d(2026, 10, 1), KindAuto),      // giornaliero (5 giorni)
		touch(t, e, d(2026, 9, 23), KindAuto),      // settimana W39: il più recente
		touch(t, e, d(2026, 7, 20), KindAuto),      // luglio: il più recente
		touch(t, e, d(2025, 9, 1), KindManual),     // manuale: mai toccato
		touch(t, e, d(2025, 9, 1), KindPreRestore), // pre-ripristino: mai toccato
	}
	drop := []string{
		touch(t, e, d(2026, 9, 21), KindAuto), // stessa W39, più vecchio
		touch(t, e, d(2026, 7, 10), KindAuto), // stesso mese, più vecchio
		touch(t, e, d(2025, 9, 1), KindAuto),  // oltre un anno
	}

	deleted, err := e.s.Prune(e.clock)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(deleted)
	sort.Strings(drop)
	if strings.Join(deleted, ",") != strings.Join(drop, ",") {
		t.Fatalf("eliminati %v, attesi %v", deleted, drop)
	}
	for _, n := range keep {
		if _, err := e.s.Path(n); err != nil {
			t.Errorf("%s doveva restare", n)
		}
	}
}

func TestSchedulerTick(t *testing.T) { // Review Focus #4
	e := newEnv(t)
	countAuto := func() int {
		n := 0
		list, _ := e.s.List()
		for _, b := range list {
			if b.Kind == KindAuto {
				n++
			}
		}
		return n
	}

	e.s.tick(24 * time.Hour)
	if countAuto() != 1 {
		t.Fatal("primo giro: atteso un backup automatico")
	}
	e.advance(time.Hour)
	e.s.tick(24 * time.Hour) // come un riavvio un'ora dopo
	if countAuto() != 1 {
		t.Fatal("intervallo non scaduto: nessun nuovo backup")
	}
	e.advance(24 * time.Hour)
	e.s.tick(24 * time.Hour)
	if countAuto() != 2 {
		t.Fatal("intervallo scaduto: atteso un secondo backup")
	}
}

func TestSchedulerDisabled(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.s.Scheduler(ctx, 0)
	if list, _ := e.s.List(); len(list) != 0 {
		t.Fatal("intervallo 0: nessun backup automatico")
	}
}
