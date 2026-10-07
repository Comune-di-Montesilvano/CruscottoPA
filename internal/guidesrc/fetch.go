package guidesrc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"
)

var (
	ErrNotFound = errors.New("File non trovato su GitHub (404).")
	ErrTooBig   = errors.New("File troppo grande (massimo 1 MB).")
	ErrNotText  = errors.New("Il file non è testo UTF-8.")
)

const rawHost = "raw.githubusercontent.com"

// Fetcher scarica un file raw: timeout, dimensione massima, redirect solo
// verso raw.githubusercontent.com (niente richieste verso la rete interna).
type Fetcher struct {
	Client    *http.Client
	MaxBytes  int64
	allowHost func(host string) bool // nei test: host del server finto
}

func NewFetcher() *Fetcher {
	f := &Fetcher{MaxBytes: 1 << 20, allowHost: func(h string) bool { return h == rawHost }}
	f.Client = &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: 10 * time.Second},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 || req.URL.Scheme != "https" || !f.allowHost(req.URL.Host) {
				return fmt.Errorf("redirect non ammesso verso %s", req.URL.Host)
			}
			return nil
		},
	}
	return f
}

func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "CruscottoPA")
	resp, err := f.Client.Do(req)
	if err != nil {
		var ue interface{ Unwrap() error }
		if errors.As(err, &ue) && ue.Unwrap() != nil {
			err = ue.Unwrap() // *url.Error: senza ripetere metodo e URL
		}
		return "", fmt.Errorf("GitHub non raggiungibile: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("GitHub ha risposto %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, f.MaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("download interrotto: %w", err)
	}
	if int64(len(data)) > f.MaxBytes {
		return "", ErrTooBig
	}
	if !utf8.Valid(data) {
		return "", ErrNotText
	}
	return string(data), nil
}
