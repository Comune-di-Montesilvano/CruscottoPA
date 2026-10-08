package identity

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity/ntlmtest"
)

func TestMessageType(t *testing.T) {
	if MessageType(ntlmtest.Negotiate()) != 1 || MessageType(ntlmtest.Authenticate("D", "u", "w")) != 3 || MessageType(Challenge()) != 2 {
		t.Fatal("tipi NTLM non riconosciuti")
	}
	for _, bad := range [][]byte{nil, []byte("NTLMSSP"), []byte("XXXXXXXX\x01\x00\x00\x00")} {
		if MessageType(bad) != 0 {
			t.Errorf("MessageType(%q) != 0", bad)
		}
	}
}

func TestChallengeIsRandom(t *testing.T) {
	a, b := Challenge(), Challenge()
	if bytes.Equal(a[24:32], b[24:32]) {
		t.Fatal("la sfida deve cambiare a ogni chiamata")
	}
}

func TestParseAuthenticate(t *testing.T) {
	got, err := ParseAuthenticate(ntlmtest.Authenticate("COMUNE-MS", "mrossi", "PC-PROVA-001"))
	want := Login{Domain: "COMUNE-MS", User: "mrossi", Workstation: "PC-PROVA-001"}
	if err != nil || got != want {
		t.Fatalf("ParseAuthenticate = %+v, %v", got, err)
	}
}

func TestParseAuthenticateRejectsHostile(t *testing.T) {
	good := ntlmtest.Authenticate("D", "utente", "W")
	setField := func(at int, length uint16, off uint32) []byte {
		m := append([]byte{}, good...)
		binary.LittleEndian.PutUint16(m[at:], length)
		binary.LittleEndian.PutUint32(m[at+4:], off)
		return m
	}
	cases := map[string][]byte{
		"vuoto":             nil,
		"troncato":          good[:40],
		"tipo 1":            ntlmtest.Negotiate(),
		"offset oltre":      setField(36, 4, uint32(len(good))),
		"offset enorme":     setField(36, 4, 0xFFFFFFF0),
		"lunghezza dispari": setField(36, 3, 72),
		"utente vuoto":      ntlmtest.Authenticate("D", "", "W"),
	}
	for name, msg := range cases {
		if _, err := ParseAuthenticate(msg); !errors.Is(err, ErrNTLM) {
			t.Errorf("%s: atteso ErrNTLM, ottenuto %v", name, err)
		}
	}
}

func FuzzParseAuthenticate(f *testing.F) {
	f.Add(ntlmtest.Authenticate("D", "u", "w"))
	f.Add(ntlmtest.Negotiate())
	f.Fuzz(func(t *testing.T, msg []byte) {
		ParseAuthenticate(msg) // non deve mai andare in panic
		MessageType(msg)
	})
}
