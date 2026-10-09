package guidesrc

import "testing"

func TestParseGitHubURL(t *testing.T) {
	src, err := ParseGitHubURL("https://github.com/org/repo/blob/main/docs/guida.md")
	if err != nil {
		t.Fatal(err)
	}
	want := Source{
		Raw:       "https://raw.githubusercontent.com/org/repo/main/docs/guida.md",
		LinkBase:  "https://github.com/org/repo/blob/main/docs/",
		ImageBase: "https://raw.githubusercontent.com/org/repo/main/docs/",
		LinkRoot:  "https://github.com/org/repo/blob/main/",
		ImageRoot: "https://raw.githubusercontent.com/org/repo/main/",
	}
	if src != want {
		t.Fatalf("%+v", src)
	}
	if src, err := ParseGitHubURL("https://github.com/o/r/blob/v1.2/README.MD"); err != nil || src.LinkBase != "https://github.com/o/r/blob/v1.2/" {
		t.Fatalf("file in radice: %+v %v", src, err)
	}
	for _, bad := range []string{
		"http://github.com/o/r/blob/main/a.md",
		"https://gitlab.com/o/r/blob/main/a.md",
		"https://user@github.com/o/r/blob/main/a.md",
		"https://github.com:8443/o/r/blob/main/a.md",
		"https://github.com/o/r/tree/main/docs",
		"https://github.com/o/r/blob/main/a.txt",
		"https://github.com/o/r/blob/main/../x/a.md",
		"https://github.com/o/r/blob/main/a.md?x=1",
		"https://github.com/o/r/blob/main/Posta Elettronica.md",
		"https://raw.githubusercontent.com/o/r/main/a.md",
		"https://github.com/o/r",
		"https://github.com/o/r/blob/main/.md/../a.md",
		"",
	} {
		if _, err := ParseGitHubURL(bad); err == nil {
			t.Errorf("%q accettato", bad)
		}
	}
}
