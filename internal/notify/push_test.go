package notify

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func testSubscription(t *testing.T, endpoint string) database.PushSubscription {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	return database.PushSubscription{
		Endpoint: endpoint,
		P256dh:   base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		Auth:     base64.RawURLEncoding.EncodeToString(auth),
	}
}

func testPusher(t *testing.T) *WebPusher {
	t.Helper()
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	return &WebPusher{Subject: "mailto:supporto@example.it", PublicKey: pub, PrivateKey: priv}
}

func TestWebPusherSends(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	p := testPusher(t)
	gone, err := p.Send(context.Background(), testSubscription(t, srv.URL+"/push/1"), []byte(`{"title":"x"}`), true)
	if err != nil || gone {
		t.Fatalf("invio: gone=%v err=%v", gone, err)
	}
	if got.Header.Get("Urgency") != "high" || got.Header.Get("TTL") != "3600" || got.Header.Get("Content-Encoding") != "aes128gcm" {
		t.Fatalf("header: %v", got.Header)
	}
	if a := got.Header.Get("Authorization"); !strings.HasPrefix(a, "vapid t=") {
		t.Fatalf("Authorization VAPID mancante: %q", a)
	}
}

func TestWebPusherGone(t *testing.T) {
	for _, code := range []int{http.StatusGone, http.StatusNotFound} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
		gone, err := testPusher(t).Send(context.Background(), testSubscription(t, srv.URL), []byte(`{}`), false)
		srv.Close()
		if !gone || err != nil {
			t.Errorf("%d: atteso gone senza errore, ottenuto %v %v", code, gone, err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer srv.Close()
	if gone, err := testPusher(t).Send(context.Background(), testSubscription(t, srv.URL), []byte(`{}`), false); gone || err == nil {
		t.Fatalf("500: atteso errore e iscrizione da tenere, ottenuto %v %v", gone, err)
	}
}

func TestPayload(t *testing.T) {
	a := database.Alert{ID: 7, Title: "Sciopero", Body: strings.Repeat("parola ", 60), Level: database.LevelUrgent, StartsAt: time.Now()}
	var m map[string]string
	if err := json.Unmarshal(Payload(a), &m); err != nil {
		t.Fatal(err)
	}
	if m["title"] != "Sciopero" || m["url"] != "/" || m["tag"] != "avviso-7" || len([]rune(m["body"])) > 160 {
		t.Fatalf("payload: %+v", m)
	}
}
