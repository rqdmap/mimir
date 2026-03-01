package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/local/oc-manager/internal/model"
)

func TestIdeasViewEmpty(t *testing.T) {
	v := NewIdeasView(80, 24)
	if len(v.ideas) != 0 {
		t.Errorf("expected 0 ideas, got %d", len(v.ideas))
	}

	// View should contain "No ideas yet"
	output := v.View()
	if output == "" {
		t.Error("expected non-empty view output")
	}
	// Simple string check (might be wrapped/styled, but text should be present)
	// We can't easily check rendered output for exact string without stripping ansi,
	// but we can check if it runs without panic.
}

func TestIdeasViewWithData(t *testing.T) {
	ideas := []model.Idea{
		{ID: "1", Content: "Idea 1", TimeCreated: time.Now().UnixMilli()},
		{ID: "2", Content: "Idea 2", TimeCreated: time.Now().UnixMilli()},
	}
	v := NewIdeasView(80, 24)
	v.SetIdeas(ideas)

	if len(v.ideas) != 2 {
		t.Errorf("expected 2 ideas, got %d", len(v.ideas))
	}

	// Test Update with navigation
	// Initial selection is 0
	if v.list.Index() != 0 {
		t.Errorf("expected index 0, got %d", v.list.Index())
	}

	// Send 'j' to move down
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}
	v, _ = v.Update(msg)

	if v.list.Index() != 1 {
		t.Errorf("expected index 1 after 'j', got %d", v.list.Index())
	}

	// Send 'k' to move up
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}
	v, _ = v.Update(msg)

	if v.list.Index() != 0 {
		t.Errorf("expected index 0 after 'k', got %d", v.list.Index())
	}
}

func TestIdeasViewDelete(t *testing.T) {
	ideas := []model.Idea{
		{ID: "1", Content: "Idea to delete", TimeCreated: time.Now().UnixMilli()},
	}
	v := NewIdeasView(80, 24)
	v.SetIdeas(ideas)

	// Send 'd'
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}
	v, _ = v.Update(msg)

	if !v.confirmDel {
		t.Error("expected confirmDel to be true after 'd'")
	}
	if v.deleteTarget != "1" {
		t.Errorf("expected deleteTarget to be '1', got %s", v.deleteTarget)
	}

	// Send 'n' to cancel
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}
	v, _ = v.Update(msg)

	if v.confirmDel {
		t.Error("expected confirmDel to be false after 'n'")
	}

	// Send 'd' again
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}
	v, _ = v.Update(msg)

	// Send 'y' to confirm
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}
	var cmd tea.Cmd
	v, cmd = v.Update(msg)

	if v.confirmDel {
		t.Error("expected confirmDel to be false after 'y'")
	}

	// Check command
	if cmd == nil {
		t.Error("expected command after 'y', got nil")
	} else {
		msg := cmd()
		if _, ok := msg.(DeleteIdeaConfirmedMsg); !ok {
			t.Errorf("expected DeleteIdeaConfirmedMsg, got %T", msg)
		}
	}
}

func TestIdeasViewEdit(t *testing.T) {
	ideas := []model.Idea{
		{ID: "1", Content: "Idea to edit", TimeCreated: time.Now().UnixMilli()},
	}
	v := NewIdeasView(80, 24)
	v.SetIdeas(ideas)

	// Send 'e'
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")}
	_, cmd := v.Update(msg)

	if cmd == nil {
		t.Error("expected command after 'e', got nil")
	} else {
		msg := cmd()
		if _, ok := msg.(EditIdeaMsg); !ok {
			t.Errorf("expected EditIdeaMsg, got %T", msg)
		}
	}
}
