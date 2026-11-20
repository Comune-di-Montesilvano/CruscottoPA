package web

import (
	"context"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/otrs"
)

// statsMock: tre ticket di ottobre (risposta dopo 1 h, dopo 3 h, nessuna),
// uno chiuso; uno del 20 settembre (negli ultimi 30 giorni dal 6 ottobre)
// chiuso; uno di agosto, fuori dal periodo.
func statsMock() *otrs.Mock {
	m := otrs.NewMock()
	oct := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	add := func(id string, created time.Time, closed bool, replyAfter time.Duration) {
		t := &otrs.Ticket{Summary: otrs.Summary{TicketID: id, Created: created, Changed: oct(5, 8), Closed: closed},
			Articles: []otrs.Article{{Created: created}}}
		if replyAfter > 0 {
			t.Articles = append(t.Articles, otrs.Article{FromAgent: true, Created: created.Add(replyAfter)})
		}
		m.Tickets[id] = t
	}
	add("1", oct(2, 8), true, time.Hour)
	add("2", oct(3, 8), false, 3*time.Hour)
	add("3", oct(4, 8), false, 0)
	add("0", time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC), true, time.Hour)
	add("9", time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC), false, time.Hour)
	return m
}

func TestTicketStatsOnce(t *testing.T) {
	s, _ := ticketTestServer(t, statsMock())
	if err := s.ticketStatsOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, ok, _ := s.db.GetTicketStats()
	if !ok || st.Requests != 4 || st.Resolved != 2 || st.MedianFirst == nil || *st.MedianFirst != time.Hour {
		t.Fatalf("statistiche: %+v", st)
	}
}

// countingStats conta le letture di FirstReply.
type countingStats struct {
	*otrs.Mock
	reads map[string]int
}

func (c *countingStats) FirstReply(ctx context.Context, id string) (time.Time, *time.Time, error) {
	c.reads[id]++
	return c.Mock.FirstReply(ctx, id)
}

func TestTicketStatsDoesNotRereadAnswered(t *testing.T) {
	c := &countingStats{Mock: statsMock(), reads: map[string]int{}}
	s, _ := ticketTestServer(t, c)
	s.ticketStatsOnce(context.Background())
	s.ticketStatsOnce(context.Background())
	if c.reads["1"] != 1 || c.reads["2"] != 1 || c.reads["3"] != 2 {
		t.Fatalf("letture: %v", c.reads)
	}
}

func TestTicketStatsDisabled(t *testing.T) {
	s, _ := ticketTestServer(t, statsMock())
	s.cfg.StatsEnabled = false
	ctx, cancel := context.WithCancel(context.Background())
	s.StartTicketStats(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()
	if _, ok, _ := s.db.GetTicketStats(); ok {
		t.Fatal("statistiche scritte con STATS_ENABLED=false")
	}
}
