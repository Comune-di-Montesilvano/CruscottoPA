package notify

import (
	"errors"
	"sync"
)

// ErrTooMany: limite di connessioni SSE raggiunto (o hub chiuso).
var ErrTooMany = errors.New("notify: troppe connessioni")

// Event è ciò che una plancia aperta riceve quando parte una notifica.
type Event struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Level string `json:"level"`
}

// Client è una plancia collegata a /eventi.
type Client struct {
	Username string
	Events   <-chan Event
	ch       chan Event
}

// Hub tiene le connessioni SSE aperte.
type Hub struct {
	mu      sync.Mutex
	clients map[*Client]bool
	max     int
	done    chan struct{}
	closed  bool
}

func NewHub(max int) *Hub {
	return &Hub{clients: map[*Client]bool{}, max: max, done: make(chan struct{})}
}

func (h *Hub) Subscribe(username string) (*Client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.clients) >= h.max {
		return nil, ErrTooMany
	}
	ch := make(chan Event, 8)
	c := &Client{Username: username, Events: ch, ch: ch}
	h.clients[c] = true
	return c, nil
}

func (h *Hub) Unsubscribe(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// Broadcast invia e alle plance per cui visible(username) è vero. Un client
// lento perde l'evento invece di bloccare gli altri.
func (h *Hub) Broadcast(e Event, visible func(username string) bool) {
	h.mu.Lock()
	targets := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		targets = append(targets, c)
	}
	h.mu.Unlock()
	for _, c := range targets {
		if !visible(c.Username) {
			continue
		}
		select {
		case c.ch <- e:
		default:
		}
	}
}

// Done si chiude con Close: gli handler SSE terminano (shutdown rapido).
func (h *Hub) Done() <-chan struct{} { return h.done }

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		close(h.done)
	}
}
