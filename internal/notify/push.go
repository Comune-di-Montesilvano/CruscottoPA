// Package notify invia le notifiche degli avvisi: in tempo reale alle plance
// aperte (SSE) e via Web Push ai browser iscritti.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/markdown"
)

// pushHosts: servizi push dei browser. Il server invia solo lì: un endpoint
// scelto da chiunque farebbe partire richieste verso host arbitrari.
var pushHosts = []string{"fcm.googleapis.com", ".notify.windows.com", "updates.push.services.mozilla.com", ".push.apple.com"}

// AllowedEndpoint: https, porta predefinita, host di un servizio push noto.
func AllowedEndpoint(endpoint string) bool {
	if len(endpoint) > 1024 {
		return false
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range pushHosts {
		if host == h || (strings.HasPrefix(h, ".") && strings.HasSuffix(host, h)) {
			return true
		}
	}
	return false
}

// Pusher consegna un messaggio a un'iscrizione Web Push. gone = iscrizione
// non più valida (da cancellare).
type Pusher interface {
	Send(ctx context.Context, sub database.PushSubscription, payload []byte, urgent bool) (gone bool, err error)
}

// WebPusher usa webpush-go (cifratura RFC 8291, firma VAPID).
type WebPusher struct {
	Subject    string // VAPID_SUBJECT così come configurato (mailto:… o https:…)
	PublicKey  string
	PrivateKey string
	Client     webpush.HTTPClient // nil = pushClient
}

// pushClient non segue i redirect (resterebbero fuori dall'elenco degli host
// ammessi) e non aspetta all'infinito un servizio che non risponde.
var pushClient = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func (p *WebPusher) Send(ctx context.Context, sub database.PushSubscription, payload []byte, urgent bool) (bool, error) {
	urgency := webpush.UrgencyNormal
	if urgent {
		urgency = webpush.UrgencyHigh
	}
	client := p.Client
	if client == nil {
		client = pushClient
	}
	resp, err := webpush.SendNotificationWithContext(ctx, payload,
		&webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth}},
		&webpush.Options{
			HTTPClient: client,
			// La libreria antepone "mailto:" a tutto ciò che non inizia con
			// "https:": va tolto qui, altrimenti diventerebbe "mailto:mailto:…".
			Subscriber:      strings.TrimPrefix(p.Subject, "mailto:"),
			VAPIDPublicKey:  p.PublicKey,
			VAPIDPrivateKey: p.PrivateKey,
			TTL:             3600,
			Urgency:         urgency,
		})
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return true, nil
	case resp.StatusCode >= 300:
		return false, fmt.Errorf("servizio push: %s", resp.Status)
	}
	return false, nil
}

// ProbePayload: notifica di prova dall'admin. Tag diverso a ogni prova: con
// lo stesso tag sostituirebbe in silenzio quella precedente ancora presente
// nel centro notifiche.
func ProbePayload(now time.Time) []byte {
	b, _ := json.Marshal(map[string]string{
		"title": "Notifica di prova",
		"body":  "Le notifiche di CruscottoPA funzionano.",
		"url":   "/",
		"tag":   fmt.Sprintf("prova-%d", now.UnixNano()),
	})
	return b
}

// Payload: contenuto della notifica (letto dal service worker).
func Payload(a database.Alert) []byte {
	b, _ := json.Marshal(map[string]string{
		"title": a.Title,
		"body":  markdown.Plain(a.Body, 120),
		"url":   fmt.Sprintf("/avvisi/%d", a.ID),
		"tag":   fmt.Sprintf("avviso-%d", a.ID),
	})
	return b
}
