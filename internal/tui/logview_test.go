package tui

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"periscope/internal/resource"
	"periscope/internal/resource/mock"
)

// loaded is the app on the mock with every status loaded, inside ns.
func loaded(t *testing.T, ns string) Model {
	t.Helper()
	p := mock.New()
	m := New(context.Background(), p, WithNamespace(ns))
	c, _ := p.Tree(context.Background())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	next, _ = next.(Model).Update(treeLoadedMsg{p, c, nil})
	m = next.(Model)
	var mu sync.Mutex
	var wg sync.WaitGroup
	msgs := []statusLoadedMsg{}
	for path := range m.byPath { // the mock answers slowly: all at once
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := p.Status(context.Background(), path)
			mu.Lock()
			msgs = append(msgs, statusLoadedMsg{p, path, st, err})
			mu.Unlock()
		}()
	}
	wg.Wait()
	for _, msg := range msgs {
		next, _ = m.Update(msg)
		m = next.(Model)
	}
	return m
}

func press(m Model, k string) Model {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	switch k {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestPreviewFollowsTheSelection(t *testing.T) {
	m := loaded(t, "shop")
	sel := m.tree.Selected()
	if sel == nil || sel.Name != "api" {
		t.Fatalf("selected %v", sel)
	}
	tg := m.logs.Target()
	if tg.Pod == nil || tg.Pod.Name != "api-7b8d9f6c5-zp6qx" || !tg.Previous {
		t.Fatalf("api's preview is its crashlooping pod's crashed run, got %+v", tg)
	}
	if m.logStarted || !m.keys.logs {
		t.Error("the log opens once the selection rests; v is offered")
	}
	next, cmd := m.Update(logDelayMsg{m.logDelay})
	m = next.(Model)
	if !m.logStarted || m.logID == 0 || cmd == nil {
		t.Fatal("after the delay the log is opened")
	}

	// lines of a log that was closed meanwhile are dropped
	stale := m.logID
	m.tree.Down() // web
	next, _ = m.Update(logLinesMsg{id: 0})
	m = next.(Model)
	if m.logs.Target().Pod.Name == "api-7b8d9f6c5-zp6qx" || m.logStarted {
		t.Fatalf("moving on retargets the preview: %+v", m.logs.Target())
	}
	next, _ = m.Update(logLinesMsg{id: stale, lines: []resource.LogLine{{Text: "old"}}})
	m = next.(Model)
	if strings.Contains(strings.Join(m.logs.Preview(80, 10), "\n"), "old") {
		t.Error("lines of a closed log must be dropped")
	}

	// a section has no log and no v
	for m.tree.Selected().Kind != resource.KindSection {
		m.tree.Down()
	}
	next, _ = m.Update(nil)
	m = next.(Model)
	if m.keys.logs || m.logs.Target().Pod != nil {
		t.Error("no log for a section")
	}
}

func TestLogView(t *testing.T) {
	m := loaded(t, "shop")
	m = press(m, "v")
	if !m.logView || !m.logStarted {
		t.Fatal("v opens the log view at once")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "kind-dev › shop › api-7b8d9f6c5-zp6qx") || !strings.Contains(view, "previous run") {
		t.Errorf("the header names the pod and the run:\n%s", view)
	}
	next, _ := m.Update(logLinesMsg{id: m.logID, lines: []resource.LogLine{{Text: "panic: boom"}}, end: true})
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.View()), "panic: boom") {
		t.Error("the lines are shown")
	}
	m = press(m, "p")
	if m.logs.Target().Previous {
		t.Error("p shows the current run")
	}
	if !strings.Contains(ansi.Strip(m.View()), "app is waiting to start (CrashLoopBackOff)") {
		t.Error("the current run of a waiting container says so")
	}
	m = press(m, "q")
	if m.logView {
		t.Fatal("q goes back to the tree")
	}
	if tg := m.logs.Target(); !tg.Previous || m.logStarted {
		t.Errorf("back in the tree, the preview shows the picked log again: %+v", tg)
	}
}

func TestLogViewContainers(t *testing.T) {
	m := loaded(t, "payments")
	m.tree.Down() // fraud
	m = press(m, "v")
	if tg := m.logs.Target(); tg.Pod.Name != "fraud-6f7d8c9b5-vx9ld" || tg.Container != "envoy" {
		t.Fatalf("fraud's not ready pod, its envoy: %+v", tg)
	}
	m = press(m, "tab")
	if tg := m.logs.Target(); tg.Container != "app" {
		t.Errorf("tab shows the next container, got %s", tg.Container)
	}
	m = press(m, "/")
	for _, r := range "zzz" {
		m = press(m, string(r))
	}
	if m.logs.Filter() != "zzz" {
		t.Errorf("filter %q", m.logs.Filter())
	}
	m = press(m, "esc")
	if m.logFiltering || m.logs.Filter() != "" || !m.logView {
		t.Error("esc closes the filter first")
	}
	m = press(m, "esc")
	if m.logView {
		t.Error("then esc goes back")
	}
}

// The clipboard sequence goes to the terminal given, not to os.Stderr,
// which the Kubernetes provider captures.
func TestCopyWritesToTheTerminal(t *testing.T) {
	m := loaded(t, "shop")
	var term bytes.Buffer
	m.clipboard = &term
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = next.(Model)
	runAll(cmd)
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("deployment/api")) + "\x07"
	if term.String() != want {
		t.Errorf("wrote %q, want %q", term.String(), want)
	}
}

// runAll runs cmd and the commands of a batch, except ticks.
func runAll(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				runAll(c)
			}
		}
	case <-time.After(100 * time.Millisecond): // a tick
	}
}

func TestDragCopiesLines(t *testing.T) {
	m := loaded(t, "shop")
	var term bytes.Buffer
	m.clipboard = &term
	m.tree.Down() // web: a running pod with a log that goes on
	m = press(m, "v")
	var lines []resource.LogLine
	for i := range 50 {
		lines = append(lines, resource.LogLine{Text: fmt.Sprintf("line %d", i)})
	}
	next, _ := m.Update(logLinesMsg{id: m.logID, lines: lines})
	m = next.(Model)
	mouse := func(a tea.MouseAction, y int) tea.Cmd {
		next, cmd := m.Update(tea.MouseMsg{X: 5, Y: y, Action: a, Button: tea.MouseButtonLeft})
		m = next.(Model)
		return cmd
	}
	mouse(tea.MouseActionPress, 2)
	mouse(tea.MouseActionMotion, 4)
	runAll(mouse(tea.MouseActionRelease, 4))
	if m.dragging || !m.logs.Selecting() {
		t.Fatal("the release ends the drag, the lines stay selected")
	}
	b64 := strings.TrimSuffix(strings.TrimPrefix(term.String(), "\x1b]52;c;"), "\x07")
	got, _ := base64.StdEncoding.DecodeString(b64)
	if n := strings.Count(string(got), "\n") + 1; n != 3 || !strings.HasPrefix(string(got), "line ") {
		t.Errorf("copied %q", got)
	}
	m = press(m, "esc")
	if m.logs.Selecting() || !m.logView {
		t.Error("esc drops the selection first")
	}
}
