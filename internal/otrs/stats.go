package otrs

import (
	"context"
	"sort"
	"time"
)

// Statistiche dell'assistenza: solo ID e date dei ticket della coda, mai il
// contenuto.

const maxStatsSearch = 10000

// MonthTicketIDs: ticket della coda creati da from e ticket chiusi da from.
func (c *HTTPClient) MonthTicketIDs(ctx context.Context, from time.Time) (createdIDs, closedIDs []string, err error) {
	since := from.In(c.loc()).Format(timeLayout)
	if createdIDs, err = c.search(ctx, map[string]any{"TicketCreateTimeNewerDate": since, "Limit": maxStatsSearch}); err != nil {
		return nil, nil, err
	}
	if closedIDs, err = c.search(ctx, map[string]any{"StateType": []string{"closed"}, "TicketCloseTimeNewerDate": since, "Limit": maxStatsSearch}); err != nil {
		return nil, nil, err
	}
	return createdIDs, closedIDs, nil
}

// FirstReply: creazione del ticket e prima risposta dell'operatore visibile al
// cliente. Il primo articolo è l'apertura (anche se scritta da un operatore).
func (c *HTTPClient) FirstReply(ctx context.Context, id string) (created time.Time, reply *time.Time, err error) {
	r, err := c.getRaw(ctx, id, false)
	if err != nil {
		return time.Time{}, nil, err
	}
	if string(r.Queue) != c.Config.Queue {
		return time.Time{}, nil, ErrNotYours
	}
	for i, a := range r.Article {
		if i == 0 || !visibleTypes[string(a.ArticleType)] || string(a.SenderType) != "agent" || toThirdParty(a, string(r.CustomerUserID)) {
			continue
		}
		t := c.parseTime(a.Created)
		return c.parseTime(r.Created), &t, nil
	}
	return c.parseTime(r.Created), nil, nil
}

func (m *Mock) MonthTicketIDs(_ context.Context, from time.Time) (createdIDs, closedIDs []string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, nil, m.Err
	}
	createdIDs, closedIDs = []string{}, []string{}
	for id, t := range m.Tickets {
		if !t.Created.Before(from) {
			createdIDs = append(createdIDs, id)
		}
		if t.Closed && !t.Changed.Before(from) {
			closedIDs = append(closedIDs, id)
		}
	}
	sort.Strings(createdIDs)
	sort.Strings(closedIDs)
	return createdIDs, closedIDs, nil
}

func (m *Mock) FirstReply(_ context.Context, id string) (created time.Time, reply *time.Time, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return time.Time{}, nil, m.Err
	}
	t, ok := m.Tickets[id]
	if !ok {
		return time.Time{}, nil, ErrNotYours
	}
	for i, a := range t.Articles {
		if i > 0 && a.FromAgent && !a.ToThirdParty {
			at := a.Created
			return t.Created, &at, nil
		}
	}
	return t.Created, nil, nil
}
