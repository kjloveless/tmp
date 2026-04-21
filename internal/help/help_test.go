package help

import (
	"testing"

	"charm.land/bubbles/v2/key"
)

func TestNewHelpUIPanicsOnDuplicateScopeBindings(t *testing.T) {
	keys := DefaultKeyMap
	keys.Queue.Down = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "down"))
	keys.Queue.DequeueSelected = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dequeue"))

	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate queue key binding panic")
		}
	}()

	_ = NewHelpUI(keys)
}

func TestContextualBindingsIncludesFocusedComponent(t *testing.T) {
	hu := NewDefault()
	tracksBindings := hu.contextualBindings(FocusTracks)
	queueBindings := hu.contextualBindings(FocusQueue)

	if got, want := len(queueBindings), len(tracksBindings); got != want {
		t.Fatalf("queue help binding count = %d, want %d", got, want)
	}
	if desc := tracksBindings[len(tracksBindings)-1].Help().Desc; desc != "queue selected" {
		t.Fatalf("tracks contextual action = %q, want queue selected", desc)
	}
	if desc := queueBindings[len(queueBindings)-1].Help().Desc; desc != "dequeue selected" {
		t.Fatalf("queue contextual action = %q, want dequeue selected", desc)
	}
	for _, b := range queueBindings {
		switch b.Help().Desc {
		case "move up", "move down":
			t.Fatalf("queue contextual help should only swap the action, got movement binding %q", b.Help().Desc)
		}
	}
}
