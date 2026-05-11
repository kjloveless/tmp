package help

import (
	"testing"

	"charm.land/bubbles/v2/key"
)

func TestDefaultLoopBindingUsesLKey(t *testing.T) {
	loopHelp := DefaultKeyMap.Global.Loop.Help()
	if loopHelp.Key != "l" {
		t.Fatalf("loop help key = %q, want l", loopHelp.Key)
	}
	if loopHelp.Desc != "loop mode" {
		t.Fatalf("loop help desc = %q, want loop mode", loopHelp.Desc)
	}
}

func TestDefaultPlaybackBindingsExposeSeekAndMuteKeys(t *testing.T) {
	if got := DefaultKeyMap.Global.SeekBack.Help().Key; got != "←" {
		t.Fatalf("seek back help key = %q, want ←", got)
	}
	if got := DefaultKeyMap.Global.SeekAhead.Help().Key; got != "→" {
		t.Fatalf("seek ahead help key = %q, want →", got)
	}
	if got := DefaultKeyMap.Global.Mute.Help().Desc; got != "mute" {
		t.Fatalf("mute help desc = %q, want mute", got)
	}
	if keys := DefaultKeyMap.Global.VolumeUp.Keys(); len(keys) < 2 || keys[1] != "+" {
		t.Fatalf("volume up keys = %#v, want shifted + binding", keys)
	}
	if keys := DefaultKeyMap.Global.VolumeDown.Keys(); len(keys) < 2 || keys[1] != "_" {
		t.Fatalf("volume down keys = %#v, want shifted _ binding", keys)
	}
}

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
