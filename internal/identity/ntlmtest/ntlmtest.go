// Package ntlmtest costruisce messaggi NTLM per i test di identity e web.
package ntlmtest

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
)

var sig = []byte("NTLMSSP\x00")

// Negotiate: messaggio di tipo 1 minimale.
func Negotiate() []byte {
	m := make([]byte, 32)
	copy(m, sig)
	binary.LittleEndian.PutUint32(m[8:], 1)
	binary.LittleEndian.PutUint32(m[12:], 0x00088207)
	return m
}

// Authenticate: messaggio di tipo 3 con dominio, utente e postazione in
// UTF-16LE; le risposte LM/NT sono vuote (il server non le verifica).
func Authenticate(domain, user, workstation string) []byte {
	const header = 72
	fields := [][]byte{nil, nil, u16(domain), u16(user), u16(workstation), nil}
	m := make([]byte, header)
	copy(m, sig)
	binary.LittleEndian.PutUint32(m[8:], 3)
	var payload bytes.Buffer
	for i, f := range fields { // Lm, Nt, Domain, User, Workstation, SessionKey
		at := 12 + 8*i
		binary.LittleEndian.PutUint16(m[at:], uint16(len(f)))
		binary.LittleEndian.PutUint16(m[at+2:], uint16(len(f)))
		binary.LittleEndian.PutUint32(m[at+4:], uint32(header+payload.Len()))
		payload.Write(f)
	}
	return append(m, payload.Bytes()...)
}

func u16(s string) []byte {
	var b bytes.Buffer
	for _, c := range utf16.Encode([]rune(s)) {
		binary.Write(&b, binary.LittleEndian, c)
	}
	return b.Bytes()
}
