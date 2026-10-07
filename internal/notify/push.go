// Package notify invia le notifiche degli avvisi: in tempo reale alle plance
// aperte (SSE) e via Web Push ai browser iscritti.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

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
	Client     webpush.HTTPClient // nil = http.Client predefinito
}

func (p *WebPusher) Send(ctx context.Context, sub database.PushSubscription, payload []byte, urgent bool) (bool, error) {
	urgency := webpush.UrgencyNormal
	if urgent {
		urgency = webpush.UrgencyHigh
	}
	resp, err := webpush.SendNotificationWithContext(ctx, payload,
		&webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth}},
		&webpush.Options{
			HTTPClient: p.Client,
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

// Payload: contenuto della notifica (letto dal service worker).
func Payload(a database.Alert) []byte {
	body := strings.Join(strings.Fields(a.Body), " ")
	if utf8.RuneCountInString(body) > 160 {
		body = string([]rune(body)[:157]) + "…"
	}
	b, _ := json.Marshal(map[string]string{
		"title": a.Title,
		"body":  body,
		"url":   "/",
		"tag":   fmt.Sprintf("avviso-%d", a.ID),
	})
	return b
}
