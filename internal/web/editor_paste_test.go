package web

import (
	"strings"
	"testing"
)

// Testo Markdown incollato nell'editor dell'admin (es. un avviso scritto
// altrove): va letto come Markdown, non lasciato con i «##» in chiaro.
func TestEditorPastesMarkdown(t *testing.T) {
	js := readFile(t, "../../web/editor/src/editor.js")
	for _, want := range []string{
		`getData("text/plain")`,
		"looksMarkdown(",
		`insertContent(text, { contentType: "markdown" })`,
		`isActive("codeBlock")`, // nel codice il testo resta com'è
		"richHTML(",             // HTML ricco (Word, pagine web): incolla normale
	} {
		if !strings.Contains(js, want) {
			t.Errorf("editor.js: manca %q", want)
		}
	}
}
