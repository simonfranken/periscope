package picker

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"periscope/internal/tui/theme"
)

func list() []Item {
	return []Item{
		{Name: "kind-dev", Meta: "kind-dev"},
		{Name: "staging", Meta: "staging-eu · shop", Current: true},
		{Name: "prod-eu", Meta: "prod-eu-1 · shop"},
		{Name: "prod-us", Meta: "prod-us-1"},
	}
}

func typed(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func name(m *Model) string {
	if it := m.Selected(); it != nil {
		return it.Name
	}
	return ""
}

func newPicker() *Model { return New(theme.Default(), "Switch context", "switch", "contexts") }

func TestStartsOnTheCurrentItem(t *testing.T) {
	m := newPicker()
	m.SetItems(list())
	if name(m) != "staging" {
		t.Fatalf("selected %q, want the current one", name(m))
	}
}

func TestSelectionMovesAndClamps(t *testing.T) {
	m := newPicker()
	m.SetItems(list())
	m.Up()
	m.Up()
	if name(m) != "kind-dev" {
		t.Errorf("up at the top = %q", name(m))
	}
	m.PageDown()
	m.Down()
	if name(m) != "prod-us" {
		t.Errorf("down at the end = %q, want the last", name(m))
	}
}

func TestSearchFiltersAndAllWordsMustMatch(t *testing.T) {
	m := newPicker()
	m.SetItems(list())
	typed(m, "PROD")
	if len(m.shown) != 2 {
		t.Errorf("prod matches %d, want 2", len(m.shown))
	}
	typed(m, " us")
	if len(m.shown) != 1 || name(m) != "prod-us" {
		t.Errorf("shown = %d, selected %q", len(m.shown), name(m))
	}
	typed(m, "zzz")
	if m.Selected() != nil {
		t.Errorf("no match must select nothing, got %q", name(m))
	}
	if !strings.Contains(m.View(100, 30), "nothing matches") {
		t.Error("view does not say that nothing matches")
	}
}

func TestSelectionStaysOnTheSameItemWhenItStillMatches(t *testing.T) {
	m := newPicker()
	m.SetItems(list())
	m.Down() // prod-eu
	typed(m, "eu")
	if name(m) != "prod-eu" {
		t.Errorf("selected %q, want prod-eu kept", name(m))
	}
}

func TestViewStates(t *testing.T) {
	m := newPicker()
	if v := m.View(100, 30); !strings.Contains(v, "reading contexts") || !strings.Contains(v, "Switch context") {
		t.Errorf("loading view:\n%s", v)
	}
	m.SetErr(errors.New("no kubeconfig"))
	if v := m.View(100, 30); !strings.Contains(v, "no kubeconfig") {
		t.Errorf("error view:\n%s", v)
	}
	m.SetItems(nil)
	if v := m.View(100, 30); !strings.Contains(v, "no contexts") {
		t.Errorf("empty view:\n%s", v)
	}
	m.SetItems(list())
	v := m.View(100, 30)
	for _, want := range []string{"* staging", "staging-eu · shop", "enter switch"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
}

func TestListScrollsInASmallTerminal(t *testing.T) {
	var many []Item
	for i := range 30 {
		many = append(many, Item{Name: fmt.Sprintf("ctx-%02d", i)})
	}
	m := newPicker()
	m.SetItems(many)
	m.View(80, 12) // room for 4 rows
	for range 10 {
		m.Down()
	}
	if v := m.View(80, 12); !strings.Contains(v, "ctx-10") || strings.Contains(v, "ctx-00") {
		t.Errorf("the selection is not scrolled into view:\n%s", v)
	}
}

func TestClickSelectsARow(t *testing.T) {
	m := newPicker()
	m.SetItems(list())
	m.View(100, 30)
	x, y := m.Origin(100, 30)
	inside, hit := m.Click(x+5, y+listTop+2, 100, 30) // the third row
	if !inside || !hit || name(m) != "prod-eu" {
		t.Errorf("inside %v, hit %v, selected %q", inside, hit, name(m))
	}
	if inside, _ := m.Click(0, 0, 100, 30); inside {
		t.Error("a click outside the popup counts as inside")
	}
}
