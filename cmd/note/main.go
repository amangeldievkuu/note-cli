// Command note is a Markdown note-taking tool: notes are plain files with
// YAML frontmatter, managed from the terminal and browsable on a local,
// live-reloading web page.
package main

import (
	"os"
	"runtime/debug"

	"github.com/amangeldievkuu/note-cli/internal/cli"
)

// version is set at release time with -ldflags "-X main.version=v1.2.3".
var version string

func main() {
	os.Exit(cli.Execute(buildVersion()))
}

// buildVersion returns the release version, or the module version recorded by
// "go install", or "dev".
func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
