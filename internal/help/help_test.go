package help

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
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
	tracksView := hu.View(FocusTracks)
	queueView := hu.View(FocusQueue)

	if tracksView == queueView {
		t.Fatal("contextual short help should differ between track and queue focus")
	}
}
