package otrs

import (
	"context"
	"fmt"
	"sync"
)

// Mock: OTRS_URL=mock (sviluppo) e test. Registra i ticket in memoria.
type Mock struct {
	mu         sync.Mutex
	Sent       []NewTicket
	Err        error // se impostato, Create fallisce
	FailUpdate bool  // simula il cliente non impostato
}

func (m *Mock) Create(_ context.Context, t NewTicket) (Created, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return Created{}, m.Err
	}
	m.Sent = append(m.Sent, t)
	n := len(m.Sent)
	return Created{TicketID: fmt.Sprint(n), TicketNumber: fmt.Sprintf("20261008000000%02d", n), CustomerSet: !m.FailUpdate}, nil
}
