package render

import (
	"strings"
	"testing"
)

func TestHTML(t *testing.T) {
	src := "# Plan\n\n" +
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n" +
		"- [x] done\n- [ ] todo\n\n" +
		"See https://go.dev and a note[^1].\n\n" +
		"```go\nfunc main() {}\n```\n\n" +
		"<script>alert(1)</script>\n\n" +
		"[^1]: The footnote.\n"
	got, err := HTML(src)
	if err != nil {
		t.Fatal(err)
	}
	html := string(got)
	for _, want := range []string{
		`<h1 id="plan">Plan<a href="#plan" class="anchor">#</a></h1>`,
		`<table>`,
		`<input checked="" disabled="" type="checkbox"`,
		`<a href="https://go.dev">https://go.dev</a>`,
		`class="footnotes"`,
		`<pre class="chroma">`,
		`<span class="kd">func</span>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %s\n%s", want, html)
		}
	}
	if strings.Contains(html, "<script>") {
		t.Errorf("raw HTML passed through:\n%s", html)
	}
}

func TestChromaCSS(t *testing.T) {
	css := string(ChromaCSS())
	if !strings.Contains(css, ".chroma") || !strings.Contains(css, "@media (prefers-color-scheme: dark)") {
		t.Errorf("unexpected CSS:\n%.300s", css)
	}
}

func TestTerminalPlain(t *testing.T) {
	out, err := Terminal("# Title\n\nSome **bold** text.\n", false, 80)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("plain output has escape codes: %q", out)
	}
	if !strings.Contains(out, "Title") || !strings.Contains(out, "bold") {
		t.Errorf("output = %q", out)
	}
}
