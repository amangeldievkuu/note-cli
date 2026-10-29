// Package render turns note Markdown into HTML for the web view and into
// styled text for the terminal.
package render

import (
	"bytes"
	"fmt"
	"html/template"
	"sync"
	"time"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/glamour"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var markdown = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM, // tables, task lists, autolinks, strikethrough
		extension.Footnote,
		highlighting.NewHighlighting(
			highlighting.WithFormatOptions(chromahtml.WithClasses(true)),
		),
	),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
		parser.WithASTTransformers(util.Prioritized(headingAnchors{}, 1000)),
	),
	// Raw HTML in notes is left out rather than passed through.
)

// HTML renders Markdown to HTML.
func HTML(src string) (template.HTML, error) {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(src), &buf); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return template.HTML(buf.String()), nil
}

// headingAnchors appends a "#" permalink to every heading that has an ID.
type headingAnchors struct{}

func (headingAnchors) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		id, ok := h.AttributeString("id")
		if !ok {
			return ast.WalkSkipChildren, nil
		}
		idBytes, _ := id.([]byte)
		link := ast.NewLink()
		link.Destination = append([]byte("#"), idBytes...)
		link.SetAttributeString("class", []byte("anchor"))
		link.AppendChild(link, ast.NewString([]byte("#")))
		h.AppendChild(h, link)
		return ast.WalkSkipChildren, nil
	})
}

var chromaCSS = sync.OnceValue(func() []byte {
	f := chromahtml.New(chromahtml.WithClasses(true))
	var b bytes.Buffer
	_ = f.WriteCSS(&b, styles.Get("github"))
	b.WriteString("@media (prefers-color-scheme: dark) {\n")
	_ = f.WriteCSS(&b, styles.Get("github-dark"))
	b.WriteString("}\n")
	return b.Bytes()
})

// ChromaCSS returns the syntax highlighting stylesheet, light by default
// and dark under prefers-color-scheme: dark.
func ChromaCSS() []byte { return chromaCSS() }

// Terminal renders Markdown for the terminal. With color off it uses
// glamour's plain "notty" style; with color on it honors GLAMOUR_STYLE and
// otherwise picks a light or dark style to suit the terminal.
func Terminal(src string, color bool, width int) (string, error) {
	style := glamour.WithStandardStyle("notty")
	if color {
		style = glamour.WithEnvironmentConfig()
	}
	r, err := glamour.NewTermRenderer(style, glamour.WithWordWrap(width))
	if err != nil {
		return "", fmt.Errorf("terminal renderer: %w", err)
	}
	out, err := r.Render(src)
	if err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return out, nil
}

// RelTime formats t relative to now: "just now", "5m ago", "3h ago",
// "2d ago", then a date.
func RelTime(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case t.Year() == now.Year():
		return t.Local().Format("Jan 2")
	default:
		return t.Local().Format("Jan 2, 2006")
	}
}
