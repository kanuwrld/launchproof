package main

import (
	"context"
	"os"
	"os/signal"

	"charm.land/fang/v2"
	"charm.land/lipgloss/v2"
	"github.com/kanuwrld/launchproof/internal/cli"
)

var version = "0.2.0"

func main() {
	// Fang renders help before RunE, so its writer needs the color preference early.
	for _, arg := range os.Args[1:] {
		if arg == "--" {
			break
		}
		if arg == "--no-color" || arg == "--no-color=true" || arg == "--plain" || arg == "--plain=true" {
			_ = os.Setenv("NO_COLOR", "1")
		}
	}
	root := cli.NewRoot(os.Stdin, os.Stdout, os.Stderr, version)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := fang.Execute(ctx, root, fang.WithVersion(version), fang.WithColorSchemeFunc(colorScheme)); err != nil {
		os.Exit(1)
	}
}

func colorScheme(lightDark lipgloss.LightDarkFunc) fang.ColorScheme {
	scheme := fang.DefaultColorScheme(lightDark)
	accent := lightDark(lipgloss.Color("#345E12"), lipgloss.Color("#C5EF91"))
	scheme.Title, scheme.Program, scheme.Command, scheme.Flag = accent, accent, accent, accent
	return scheme
}
