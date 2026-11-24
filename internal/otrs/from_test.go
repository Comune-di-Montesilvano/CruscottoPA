package otrs

import "testing"

// Il web service di OTRS salva il From così com'è: un nome codificato in
// RFC 2047 (come lo scrive mail.Address.String) comparirebbe nel ticket come
// "=?utf-8?q?...?=". Il nome va in UTF-8 in chiaro.
func TestFromHeaderUTF8(t *testing.T) {
	cases := map[[2]string]string{
		{"Mario Rossi", "mrossi@example.it"}:          `"Mario Rossi" <mrossi@example.it>`,
		{"Niccolò D'Angelo", "ndangelo@example.it"}:   `"Niccolò D'Angelo" <ndangelo@example.it>`,
		{`Mario "Il" Rossi\`, "mrossi@example.it"}:    `"Mario \"Il\" Rossi\\" <mrossi@example.it>`,
		{"", "mrossi@example.it"}:                     `<mrossi@example.it>`,
		{"Riga\r\nBcc: x@example.it", "m@example.it"}: `"Riga  Bcc: x@example.it" <m@example.it>`,
	}
	for in, want := range cases {
		if got := fromHeader(in[0], in[1]); got != want {
			t.Errorf("fromHeader(%q, %q) = %q, atteso %q", in[0], in[1], got, want)
		}
	}
}
