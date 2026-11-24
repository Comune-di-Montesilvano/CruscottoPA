// Package rubrica interroga l'API di ricerca della Rubrica dell'ente
// (GET /api/cerca) per il gruppo «Persone» della ricerca a tendina.
package rubrica

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	timeout    = 2 * time.Second
	maxBody    = 256 << 10
	maxResults = 8
	cacheTTL   = 2 * time.Minute
	cacheMax   = 500
	minQuery   = 2
)

// Person: un contatto della Rubrica, con l'indirizzo della sua scheda.
type Person struct {
	UID     string `json:"uid"`
	Nome    string `json:"nome"`
	Reparto string `json:"reparto"`
	Interno string `json:"interno"`
	Mail    string `json:"mail"`
	URL     string `json:"url"`
}

type cached struct {
	at     time.Time
	people []Person
}

// Client: ricerche verso la Rubrica con cache in memoria. Il testo cercato non
// finisce mai negli errori restituiti (li scrive il chiamante nei log).
type Client struct {
	base string
	http *http.Client
	now  func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

// New restituisce nil con base vuota (funzione spenta).
func New(base string) *Client {
	if base == "" {
		return nil
	}
	return &Client{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{
			Timeout: timeout,
			// Un redirect (es. verso un login) non è una risposta dell'API.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		now:   time.Now,
		cache: map[string]cached{},
	}
}

// normalize: chiave della cache, minuscolo con spazi compattati.
func normalize(q string) string {
	return strings.ToLower(strings.Join(strings.Fields(q), " "))
}

// Search cerca q nella Rubrica (al massimo 8 persone). Testo più corto di 2
// caratteri = nessuna richiesta.
func (c *Client) Search(ctx context.Context, q string) ([]Person, error) {
	key := normalize(q)
	if utf8.RuneCountInString(key) < minQuery {
		return []Person{}, nil
	}
	now := c.now()
	c.mu.Lock()
	if e, ok := c.cache[key]; ok && now.Sub(e.at) < cacheTTL {
		c.mu.Unlock()
		return e.people, nil
	}
	c.mu.Unlock()

	people, err := c.fetch(ctx, strings.Join(strings.Fields(q), " "))
	if err != nil {
		return []Person{}, err
	}
	c.mu.Lock()
	if len(c.cache) >= cacheMax {
		c.cache = map[string]cached{}
	}
	c.cache[key] = cached{at: now, people: people}
	c.mu.Unlock()
	return people, nil
}

func (c *Client) fetch(ctx context.Context, q string) ([]Person, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/cerca?q="+url.QueryEscape(q), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("richiesta alla rubrica non riuscita")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rubrica: stato %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, errors.New("rubrica: lettura della risposta non riuscita")
	}
	if len(body) > maxBody {
		return nil, errors.New("rubrica: risposta troppo grande")
	}
	var people []Person
	if err := json.Unmarshal(body, &people); err != nil {
		return nil, errors.New("rubrica: risposta non valida")
	}
	if len(people) > maxResults {
		people = people[:maxResults]
	}
	for i := range people {
		people[i].URL = c.base + "/contacts/" + url.PathEscape(people[i].UID)
	}
	if people == nil {
		people = []Person{}
	}
	return people, nil
}
