package tui

import (
	"io"
	"os"

	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
)

// WithClipboard writes the clipboard's escape sequence to w, the terminal.
// It must be the terminal itself: the Kubernetes provider captures
// os.Stderr while the UI runs, and what is written there never arrives.
func WithClipboard(w io.Writer) Option {
	return func(m *Model) { m.clipboard = w }
}

// copyToClipboard sets the system clipboard with the OSC 52 escape sequence,
// which works over SSH and in terminals that allow it; others ignore it.
// Inside tmux it is sent as is: with set-clipboard on, tmux takes it and
// passes it on to the terminal. Wrapped for passthrough it would need
// allow-passthrough, off by default since tmux 3.3.
func (m Model) copyToClipboard(text string) tea.Cmd {
	w := m.clipboard
	if w == nil {
		w = os.Stderr
	}
	return func() tea.Msg {
		_, _ = osc52.New(text).WriteTo(w)
		return nil
	}
}
