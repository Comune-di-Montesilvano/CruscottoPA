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
	if m["title"] != "Sciopero" || m["url"] != "/avvisi/7" || m["tag"] != "avviso-7" || len([]rune(m["body"])) > 120 {
		t.Fatalf("payload: %+v", m)
	}
}

// Un servizio push che rimanda altrove non deve far partire richieste verso
// altri indirizzi (SSRF verso la rete interna).
func TestWebPusherDoesNotFollowRedirects(t *testing.T) {
	hit := false
	inner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer inner.Close()
	outer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, inner.URL, http.StatusTemporaryRedirect)
	}))
	defer outer.Close()
	gone, err := testPusher(t).Send(context.Background(), testSubscription(t, outer.URL), []byte(`{}`), false)
	if hit || gone || err == nil {
		t.Fatalf("redirect seguito o accettato: hit=%v gone=%v err=%v", hit, gone, err)
	}
}

func TestAllowedEndpoint(t *testing.T) {
	for u, want := range map[string]bool{
		"https://fcm.googleapis.com/fcm/send/abc":                 true,
		"https://wns2-par02p.notify.windows.com/w/?token=x":       true,
		"https://updates.push.services.mozilla.com/wpush/v2/x":    true,
		"https://web.push.apple.com/QGx":                          true,
		"http://fcm.googleapis.com/fcm/send/abc":                  false,
		"https://attacker.example/x":                              false,
		"https://10.0.0.5/x":                                      false,
		"https://fcm.googleapis.com.attacker.example/x":           false,
		"https://notify.windows.com.evil/x":                       false,
		"https://user@fcm.googleapis.com/x":                       false,
		"https://fcm.googleapis.com:8443/x":                       false,
		"https://fcm.googleapis.com/" + strings.Repeat("a", 1100): false,
	} {
		if got := AllowedEndpoint(u); got != want {
			t.Errorf("AllowedEndpoint(%q) = %v, atteso %v", u, got, want)
		}
	}
}

func TestPayloadPlainText(t *testing.T) {
	var p map[string]string
	json.Unmarshal(Payload(database.Alert{ID: 7, Title: "T", Body: "**Grassetto** e [link](https://x.org)"}), &p)
	if p["body"] != "Grassetto e link" {
		t.Fatalf("payload: %v", p)
	}
}

// Ogni prova ha un tag diverso: con lo stesso tag la notifica sostituirebbe in
// silenzio quella precedente rimasta nel centro notifiche.
func TestTestPayloadUniqueTag(t *testing.T) {
	var a, b map[string]string
	json.Unmarshal(ProbePayload(time.Unix(100, 0)), &a)
	json.Unmarshal(ProbePayload(time.Unix(101, 0)), &b)
	if a["title"] != "Notifica di prova" || a["url"] != "/" || a["tag"] == "" || a["tag"] == b["tag"] {
		t.Fatalf("payload di prova: %v %v", a, b)
	}
}
