// Package identity riconosce chi usa la plancia.
//
// ATTENZIONE: l'identità ricavata da NTLM è DICHIARATA dal browser e non è
// verificata (servirebbe NETLOGON verso il domain controller). Chiunque in rete
// può fabbricare un messaggio con il nome di un collega. Va usata solo per
// personalizzare la vista, MAI per autorizzare azioni o dare accesso all'admin.
package identity

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"unicode/utf16"
)

var (
	ErrNTLM = errors.New("identity: messaggio NTLM non valido")
	ntlmSig = []byte("NTLMSSP\x00")
)

// Login è quanto il browser dichiara nel messaggio NTLM di tipo 3.
type Login struct {
	Domain, User, Workstation string
}

// MessageType restituisce 1, 2 o 3; 0 se msg non è un messaggio NTLM.
func MessageType(msg []byte) int {
	if len(msg) < 12 || !bytes.HasPrefix(msg, ntlmSig) {
		return 0
	}
	switch t := binary.LittleEndian.Uint32(msg[8:12]); t {
	case 1, 2, 3:
		return int(t)
	}
	return 0
}

// Challenge costruisce un messaggio di tipo 2 minimale con sfida casuale.
func Challenge() []byte {
	name := utf16le("CRUSCOTTO")
	var info bytes.Buffer
	for _, avID := range []uint16{2, 1} { // MsvAvNbDomainName, MsvAvNbComputerName
		binary.Write(&info, binary.LittleEndian, avID)
		binary.Write(&info, binary.LittleEndian, uint16(len(name)))
		info.Write(name)
	}
	info.Write([]byte{0, 0, 0, 0}) // MsvAvEOL

	const header = 48
	msg := make([]byte, header)
	copy(msg, ntlmSig)
	binary.LittleEndian.PutUint32(msg[8:], 2)
	binary.LittleEndian.PutUint16(msg[12:], uint16(len(name))) // TargetName
	binary.LittleEndian.PutUint16(msg[14:], uint16(len(name)))
	binary.LittleEndian.PutUint32(msg[16:], header)
	// UNICODE | REQUEST_TARGET | NTLM | ALWAYS_SIGN | TARGET_TYPE_DOMAIN | EXTENDED_SESSIONSECURITY | TARGET_INFO
	binary.LittleEndian.PutUint32(msg[20:], 0x00000001|0x00000004|0x00000200|0x00008000|0x00010000|0x00080000|0x00800000)
	rand.Read(msg[24:32])                                       // da Go 1.24 crypto/rand.Read non restituisce mai errore
	binary.LittleEndian.PutUint16(msg[40:], uint16(info.Len())) // TargetInfo
	binary.LittleEndian.PutUint16(msg[42:], uint16(info.Len()))
	binary.LittleEndian.PutUint32(msg[44:], uint32(header+len(name)))
	msg = append(msg, name...)
	return append(msg, info.Bytes()...)
}

// ParseAuthenticate legge dominio, utente e postazione da un messaggio di
// tipo 3. NON verifica la risposta: vedi il commento del pacchetto.
func ParseAuthenticate(msg []byte) (Login, error) {
	if MessageType(msg) != 3 || len(msg) < 52 {
		return Login{}, ErrNTLM
	}
	var l Login
	var err error
	if l.Domain, err = field(msg, 28); err != nil {
		return Login{}, err
	}
	if l.User, err = field(msg, 36); err != nil || l.User == "" {
		return Login{}, ErrNTLM
	}
	if l.Workstation, err = field(msg, 44); err != nil {
		return Login{}, err
	}
	return l, nil
}

// field legge un campo UTF-16LE descritto da (lunghezza, max, offset) in at.
func field(msg []byte, at int) (string, error) {
	n := uint64(binary.LittleEndian.Uint16(msg[at:]))
	off := uint64(binary.LittleEndian.Uint32(msg[at+4:]))
	if n%2 != 0 || off+n > uint64(len(msg)) {
		return "", ErrNTLM
	}
	u := make([]uint16, n/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(msg[off+uint64(2*i):])
	}
	return string(utf16.Decode(u)), nil
}

func utf16le(s string) []byte {
	var b bytes.Buffer
	for _, c := range utf16.Encode([]rune(s)) {
		binary.Write(&b, binary.LittleEndian, c)
	}
	return b.Bytes()
}
