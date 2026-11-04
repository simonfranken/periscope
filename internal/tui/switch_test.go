package tui

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"periscope/internal/resource"
	"periscope/internal/resource/mock"
)

func TestMain(m *testing.M) {
	zone.NewGlobal()
	os.Exit(m.Run())
}

func TestSwitchDropsAnswersOfTheOldContext(t *testing.T) {
	old := mock.New()
	m := New(context.Background(), old)
	c, _ := old.Tree(context.Background())
	next, _ := m.Update(treeLoadedMsg{old, c, nil})
	m = next.(Model)

	p, err := old.Switch("staging")
	if err != nil {
		t.Fatal(err)
	}
	sc, _ := p.Tree(context.Background())
	m.switching = "staging"
	pctx, pcancel := context.WithCancel(context.Background())
	oldCtx := m.pctx
	next, _ = m.Update(switchedMsg{"staging", p, pctx, pcancel, sc, nil})
	m = next.(Model)
	if m.provider != p || m.cluster.Context != "staging" || m.switching != "" {
		t.Fatalf("not switched: context %q, switching %q", m.cluster.Context, m.switching)
	}
	if oldCtx.Err() == nil {
		t.Error("the old provider's requests should be cancelled")
	}
	if s := m.tree.Scope(); s == nil || s.Name != "shop" {
		t.Errorf("staging's context sets shop, scope = %v", s)
	}

	failed := &resource.Status{Phase: resource.PhaseFailed}
	next, _ = m.Update(statusLoadedMsg{old, "shop/deployment/api", failed, nil})
	m = next.(Model)
	if m.byPath["shop/deployment/api"].Status == failed {
		t.Error("a status of the old context landed in the new tree")
	}
	next, _ = m.Update(treeLoadedMsg{old, c, nil})
	if next.(Model).cluster.Context != "staging" {
		t.Error("a tree of the old context replaced the new one")
	}
}

func TestFailedSwitchKeepsTheCluster(t *testing.T) {
	old := mock.New()
	m := New(context.Background(), old)
	c, _ := old.Tree(context.Background())
	next, _ := m.Update(treeLoadedMsg{old, c, nil})
	m = next.(Model)
	m.switching = "prod-eu"
	next, _ = m.Update(switchedMsg{"prod-eu", nil, nil, nil, nil, context.DeadlineExceeded})
	m = next.(Model)
	if m.provider != old || m.cluster.Context != "kind-dev" || m.switching != "" || m.err != nil {
		t.Errorf("a failed switch must keep kind-dev: context %q, err %v", m.cluster.Context, m.err)
	}
}

func TestPendingSwitchIsShownAndBlocksThePicker(t *testing.T) {
	old := mock.New()
	m := New(context.Background(), old)
	c, _ := old.Tree(context.Background())
	next, _ := m.Update(treeLoadedMsg{old, c, nil})
	m = next.(Model)
	next, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = next.(Model)
	m.switching = "staging"
	if !strings.Contains(ansi.Strip(m.View()), "switching to staging") {
		t.Error("a pending switch must stay visible")
	}
	next, _ = m.openPicker()
	if next.(Model).picker != nil {
		t.Error("C during a switch would list the old context's contexts")
	}
}
