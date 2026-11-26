package logs

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"periscope/internal/resource"
	"periscope/internal/tui/theme"
)

func target() resource.LogTarget {
	p := &resource.Object{Kind: resource.KindPod, Namespace: "shop", Name: "api-1", Path: "shop/pod/api-1",
		Status: &resource.Status{Containers: []resource.Container{{Name: "app", State: resource.ContainerRunning}}}}
	return resource.LogTarget{Pod: p, Container: "app"}
}

func lines(n int, f string) []resource.LogLine {
	out := make([]resource.LogLine, n)
	for i := range out {
		out[i] = resource.LogLine{Time: time.Now(), Text: fmt.Sprintf(f, i)}
	}
	return out
}

func newModel(w, h int) Model {
	m := New(theme.Default())
	m.Reset(target())
	m.SetSize(w, h)
	return m
}

func visible(m Model) []string {
	return strings.Split(ansi.Strip(m.View()), "\n")
}

func TestFollowAndScroll(t *testing.T) {
	m := newModel(60, 5)
	if !m.Loading() {
		t.Fatal("a reset log is loading")
	}
	m.Append(lines(20, "line %d"))
	if m.Loading() || !m.Following() || m.offset != 15 {
		t.Fatalf("follows the end: offset %d", m.offset)
	}
	m.Append(lines(1, "new %d"))
	if v := visible(m); !strings.Contains(v[4], "new 0") {
		t.Errorf("a followed log shows new lines: %q", v[4])
	}
	m.Scroll(-3)
	if m.Following() {
		t.Error("scrolling up pauses")
	}
	off := m.offset
	m.Append(lines(5, "more %d"))
	if m.offset != off {
		t.Error("a paused view stays where it is")
	}
	m.PageDown()
	m.PageDown()
	if !m.Following() {
		t.Error("scrolling to the end follows again")
	}
	m.Home()
	if m.offset != 0 || m.Following() {
		t.Errorf("home: offset %d", m.offset)
	}
	m.End()
	if !m.Following() || m.offset != m.maxOffset() {
		t.Error("end follows")
	}
}

func TestWrapAndFilter(t *testing.T) {
	m := newModel(30, 10)
	m.Append([]resource.LogLine{{Text: strings.Repeat("word ", 12)}, {Text: "level=error short"}})
	if len(m.rows) < 3 {
		t.Fatalf("a long line wraps: %d rows", len(m.rows))
	}
	m.ToggleWrap()
	if len(m.rows) != 2 {
		t.Errorf("cut: one row per line, got %d", len(m.rows))
	}
	if v := visible(m); !strings.HasSuffix(strings.TrimRight(v[0], " "), "…") {
		t.Errorf("a cut line ends with …: %q", v[0])
	}
	m.SetFilter("ERROR")
	if len(m.rows) != 1 || m.matching != 1 {
		t.Errorf("the filter ignores case: %d rows", len(m.rows))
	}
	m.SetFilter("nothing like it")
	if v := visible(m); !strings.Contains(v[0], "nothing matches") {
		t.Errorf("no match: %q", v[0])
	}
}

func TestStates(t *testing.T) {
	m := newModel(60, 5)
	m.Finish(nil)
	if v := visible(m); !strings.Contains(v[0], "the log is empty") {
		t.Errorf("an ended empty log: %q", v[0])
	}
	tg := target()
	tg.Pod.Status.Containers[0] = resource.Container{Name: "app", State: resource.ContainerWaiting, Reason: "ImagePullBackOff"}
	m.Reset(tg)
	if m.Loading() {
		t.Error("a waiting container has nothing to load")
	}
	if v := visible(m); !strings.Contains(v[0], "app is waiting to start (ImagePullBackOff)") {
		t.Errorf("waiting: %q", v[0])
	}
}

func TestKeepsTheLastLines(t *testing.T) {
	m := newModel(60, 5)
	m.Append(lines(maxLines+10, "line %d"))
	if len(m.lines) != maxLines || m.lines[0].Text != "line 10" {
		t.Errorf("kept %d lines from %q", len(m.lines), m.lines[0].Text)
	}
}

func TestPreview(t *testing.T) {
	m := newModel(60, 5)
	m.Append(lines(10, "line %d"))
	p := m.Preview(40, 4)
	if len(p) != 4 || !strings.Contains(ansi.Strip(p[0]), "Logs  app") || !strings.Contains(ansi.Strip(p[3]), "line 9") {
		t.Errorf("title and the last lines: %q", p)
	}
	m.ShowPod(true)
	if p := m.Preview(40, 4); !strings.Contains(ansi.Strip(p[0]), "api-1 · app") {
		t.Errorf("picked for a workload, the title names the pod: %q", p[0])
	}
	m.Append([]resource.LogLine{{Text: "\x1b[31mred\x1b[0m\tand\x07 tab"}})
	if got := m.lines[len(m.lines)-1].Text; got != "red    and tab" {
		t.Errorf("escape sequences and control characters are removed: %q", got)
	}
}

func TestSelection(t *testing.T) {
	m := newModel(30, 5)
	m.Append([]resource.LogLine{{Text: "a"}, {Text: strings.Repeat("long ", 10)}, {Text: "c"}, {Text: "d"}})
	// rows: a, long (3 rows), c, d; the view shows the last 5 rows
	if len(m.rows) != 6 || m.offset != 1 {
		t.Fatalf("%d rows, offset %d", len(m.rows), m.offset)
	}
	m.Press(0, false) // the first row of "long"
	if !m.Selecting() || m.Following() {
		t.Fatal("a press selects and pauses")
	}
	m.Drag(2) // its last row: still the same line
	if s, n, _ := m.SelectedText(); n != 1 || s != strings.Repeat("long ", 10) {
		t.Errorf("a wrapped line is copied whole: %q (%d)", s, n)
	}
	m.Drag(9) // below the view: scrolls, selects to the last line
	if s, n, _ := m.SelectedText(); n != 3 || !strings.HasSuffix(s, "c\nd") {
		t.Errorf("drag to the end: %q (%d)", s, n)
	}
	m.Drag(-1) // above the view: scrolls up, selects "a" too
	if _, n, _ := m.SelectedText(); n != 2 || m.anchor != 1 || m.end != 0 {
		t.Errorf("drag up: %d lines, %d..%d", n, m.anchor, m.end)
	}
	m.Press(4, true) // the view starts at "a" now: row 4 is "c"
	if m.anchor != 1 || m.end != 2 {
		t.Errorf("shift+click extends: %d..%d", m.anchor, m.end)
	}
	m.SetFilter("long")
	if m.Selecting() {
		t.Error("a new filter drops the selection")
	}
	if s, n, _ := m.ShownText(); n != 1 || !strings.HasPrefix(s, "long") {
		t.Errorf("copy all copies the matches: %q", s)
	}
}

func TestCopyLimit(t *testing.T) {
	m := newModel(30, 5)
	big := strings.Repeat("x", 1000)
	m.Append(lines(maxCopy/1000+50, big+" %d"))
	s, n, cut := m.ShownText()
	if !cut || len(s) > maxCopy || !strings.HasSuffix(s, fmt.Sprintf(" %d", len(m.lines)-1)) {
		t.Errorf("cut %v, %d bytes, %d lines: the last lines up to the limit", cut, len(s), n)
	}
	if strings.Contains(s, "\x1b") || !strings.Contains(s, ":") {
		t.Error("plain text with the time")
	}
}
