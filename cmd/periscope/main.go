// Command periscope is a TUI for Kubernetes clusters: problems loud, healthy quiet.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"periscope/internal/resource"
	"periscope/internal/resource/mock"
	"periscope/internal/tui"
	"periscope/internal/update"
)

// version is set at build time (-ldflags "-X main.version=v1.2.3").
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "update":
			os.Exit(runUpdate(os.Args[2:]))
		case "version", "--version", "-version":
			fmt.Println("periscope", version)
			return
		}
	}

	useMock := flag.Bool("mock", false, "use built-in mock data instead of a cluster")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: periscope --mock\n       periscope update [--check] [--force]\n       periscope version")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}

	var provider resource.Provider
	if *useMock {
		provider = mock.New()
	} else {
		// The Kubernetes provider (client-go) is the next step, see
		// docs/README.md; until then only the mock data can be shown.
		fmt.Fprintln(os.Stderr, "periscope: no cluster provider yet; run `periscope --mock` to see the mock cluster")
		os.Exit(2)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Detect the background (for adaptive colors) before Bubble Tea takes
	// over stdin; the result is cached by lipgloss.
	lipgloss.HasDarkBackground()

	zone.NewGlobal()
	defer zone.Close()

	var opts []tui.Option
	if !*useMock && update.Release(version) && os.Getenv("PERISCOPE_NO_UPDATE_CHECK") == "" {
		opts = append(opts, tui.WithUpdateCheck(latestRelease))
	}

	p := tea.NewProgram(tui.New(ctx, provider, opts...), tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// The user chose "quit and update": the terminal is restored by now.
	if m, ok := final.(tui.Model); ok && m.UpdateTo() != "" {
		fmt.Printf("updating to %s…\n", m.UpdateTo())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if code := install(ctx, update.New(), m.UpdateTo()); code != 0 {
			os.Exit(code)
		}
	}
}

// latestRelease returns the version of a newer release, or "" if there is
// none or GitHub can't be reached within a few seconds.
func latestRelease(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	v, err := update.New().Latest(ctx)
	if err != nil || !update.Newer(version, v) {
		return ""
	}
	return v
}

func runUpdate(args []string) int {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	check := fs.Bool("check", false, "only report whether a newer version exists")
	force := fs.Bool("force", false, "install the latest version even if not newer (also for dev builds)")
	fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	c := update.New()
	latest, err := c.Latest(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "periscope: update check failed:", err)
		return 1
	}
	if !*force && !update.Newer(version, latest) {
		fmt.Printf("periscope %s is up to date\n", version)
		return 0
	}
	if *check {
		fmt.Printf("update available: %s (you have %s)\n", latest, version)
		return 0
	}
	if !update.Release(version) && !*force {
		fmt.Fprintf(os.Stderr, "periscope: this is a %q build; use --force to replace it with %s\n", version, latest)
		return 1
	}
	return install(ctx, c, latest)
}

// install replaces the running binary with release latest.
func install(ctx context.Context, c *update.Client, latest string) int {
	exe, err := c.Install(ctx, latest)
	if err != nil {
		fmt.Fprintln(os.Stderr, "periscope: update failed:", err)
		return 1
	}
	fmt.Printf("updated %s: %s -> %s\n", exe, version, latest)
	return 0
}
