package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/filepicker"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gopxl/beep/v2"
	"github.com/kjloveless/tmp/internal/help"
	"github.com/kjloveless/tmp/internal/track"
)

type testStream struct {
	len      int
	position int
	closed   bool
}

func (s *testStream) Stream(samples [][2]float64) (int, bool) {
	return 0, false
}

func (s *testStream) Err() error {
	return nil
}

func (s *testStream) Len() int {
	return s.len
}

func (s *testStream) Position() int {
	return s.position
}

func (s *testStream) Seek(p int) error {
	s.position = p
	return nil
}

func (s *testStream) Close() error {
	s.closed = true
	return nil
}

func TestEnqueueSelectedQueuesHighlightedFile(t *testing.T) {
	dir := t.TempDir()
	playingPath := filepath.Join(dir, "a.mp3")
	selectedPath := filepath.Join(dir, "b.mp3")

	for _, path := range []string{playingPath, selectedPath} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}

	fp := filepicker.New()
	fp.CurrentDirectory = dir
	fp.AllowedTypes = []string{".mp3"}

	m := model{
		tracks:      newTracksComponent(fp),
		playing:     track.Track{Title: filepath.Base(playingPath)},
		playingPath: playingPath,
	}
	m.tracks.syncSelection(tea.KeyMsg{Type: tea.KeyDown}, fp.CurrentDirectory)
	m.enqueueSelected()

	if len(m.queue) != 1 {
		t.Fatalf("queue length = %d, want 1", len(m.queue))
	}
	if m.queue[0].path != selectedPath {
		t.Fatalf("queued path = %q, want %q", m.queue[0].path, selectedPath)
	}
	if m.queue[0].title != filepath.Base(selectedPath) {
		t.Fatalf("queued title = %q, want %q", m.queue[0].title, filepath.Base(selectedPath))
	}
}

func TestQueueSelectedOnlyQueuesWhenIdle(t *testing.T) {
	dir := t.TempDir()
	selectedPath := filepath.Join(dir, "a.mp3")
	if err := os.WriteFile(selectedPath, nil, 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", selectedPath, err)
	}

	fp := filepicker.New()
	fp.CurrentDirectory = dir
	fp.AllowedTypes = []string{".mp3"}

	m := model{
		tracks: newTracksComponent(fp),
		help:   help.NewDefault(),
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	got := updated.(model)

	if cmd != nil {
		t.Fatal("queueing while idle returned playback command, want queue-only behavior")
	}
	if len(got.queue) != 1 {
		t.Fatalf("queue length = %d, want 1", len(got.queue))
	}
	if got.queue[0].path != selectedPath {
		t.Fatalf("queued path = %q, want %q", got.queue[0].path, selectedPath)
	}
}

func TestPlayPauseStartsQueuedTrackWhenIdle(t *testing.T) {
	m := model{
		help: help.NewDefault(),
		queue: []queuedTrack{{
			path:  "next.mp3",
			title: "next.mp3",
		}},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	got := updated.(model)

	if cmd == nil {
		t.Fatal("play/pause with queued item returned nil command, want playback command")
	}
	if len(got.queue) != 0 {
		t.Fatalf("queue length = %d, want 0 after starting queued track", len(got.queue))
	}
}

func TestPlayPauseQueuesAndStartsSelectedTrackWhenIdleQueueEmpty(t *testing.T) {
	dir, err := filepath.Abs("../../sounds/mp3")
	if err != nil {
		t.Fatal(err)
	}
	selectedPath := filepath.Join(dir, "break.mp3")

	fp := filepicker.New()
	fp.CurrentDirectory = dir
	fp.AllowedTypes = []string{".mp3"}

	m := model{
		tracks: newTracksComponent(fp),
		help:   help.NewDefault(),
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	got := updated.(model)

	if cmd == nil {
		t.Fatal("play/pause with empty queue returned nil command, want selected-track playback command")
	}
	if len(got.queue) != 0 {
		t.Fatalf("queue length = %d, want 0 after starting selected track", len(got.queue))
	}

	msg := cmd()
	loaded, ok := msg.(loadedTrackMsg)
	if !ok {
		t.Fatalf("command returned %T, want loadedTrackMsg", msg)
	}
	t.Cleanup(func() {
		if err := loaded.track.Control.Source.Close(); err != nil {
			t.Fatalf("close loaded track: %v", err)
		}
	})
	if loaded.path != selectedPath {
		t.Fatalf("loaded path = %q, want %q", loaded.path, selectedPath)
	}
}

func TestQueueFocusDequeueKeyRemovesFirstQueuedTrackByDefault(t *testing.T) {
	m := model{
		help: help.NewDefault(),
		queue: []queuedTrack{
			{path: "first.mp3", title: "first.mp3"},
			{path: "second.mp3", title: "second.mp3"},
		},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	focused := updated.(model)

	updated, cmd := focused.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	got := updated.(model)

	if cmd != nil {
		t.Fatal("dequeue returned command, want nil")
	}
	if len(got.queue) != 1 {
		t.Fatalf("queue length = %d, want 1", len(got.queue))
	}
	if got.queue[0].path != "second.mp3" {
		t.Fatalf("remaining queued path = %q, want second.mp3", got.queue[0].path)
	}
}

func TestQueueFocusDequeueRemovesSelectedQueuedTrack(t *testing.T) {
	m := model{
		help: help.NewDefault(),
		queue: []queuedTrack{
			{path: "first.mp3", title: "first.mp3"},
			{path: "second.mp3", title: "second.mp3"},
			{path: "third.mp3", title: "third.mp3"},
		},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	focused := updated.(model)
	if focused.focus != focusQueue {
		t.Fatalf("focus = %v, want queue focus", focused.focus)
	}

	updated, _ = focused.Update(tea.KeyMsg{Type: tea.KeyDown})
	selected := updated.(model)
	if selected.queueCursor != 1 {
		t.Fatalf("queue cursor = %d, want 1", selected.queueCursor)
	}

	updated, cmd := selected.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	got := updated.(model)
	if cmd != nil {
		t.Fatal("dequeue selected returned command, want nil")
	}
	if len(got.queue) != 2 {
		t.Fatalf("queue length = %d, want 2", len(got.queue))
	}
	if got.queue[0].path != "first.mp3" || got.queue[1].path != "third.mp3" {
		t.Fatalf("queue paths = [%s %s], want [first.mp3 third.mp3]", got.queue[0].path, got.queue[1].path)
	}
	if got.queueCursor != 1 {
		t.Fatalf("queue cursor = %d, want 1 after removing selected item", got.queueCursor)
	}
}

func TestQueueViewShowsPlayingTrackAndKeepsFixedWidth(t *testing.T) {
	empty := (&model{}).queueView()
	withLongPlaying := (&model{
		playing: track.Track{Title: strings.Repeat("a", queuePanelContentWidth*2)},
	}).queueView()
	withQueue := (&model{
		queue: []queuedTrack{{
			path:  "next.mp3",
			title: strings.Repeat("b", queuePanelContentWidth*2),
		}},
	}).queueView()

	emptyWidth := lipgloss.Width(empty)
	if got := lipgloss.Width(withLongPlaying); got != emptyWidth {
		t.Fatalf("playing queue panel width = %d, want %d", got, emptyWidth)
	}
	if got := lipgloss.Width(withQueue); got != emptyWidth {
		t.Fatalf("queued panel width = %d, want %d", got, emptyWidth)
	}
	if !strings.Contains(withLongPlaying, "Playing") {
		t.Fatal("queue panel with active track does not show Playing section")
	}
}

func TestTruncateBlockKeepsEveryLineWithinWidth(t *testing.T) {
	const width = 10
	got := truncateBlock("short\n"+strings.Repeat("x", width*2), width)
	for _, line := range strings.Split(got, "\n") {
		if lineWidth := lipgloss.Width(line); lineWidth > width {
			t.Fatalf("line width = %d, want <= %d for %q", lineWidth, width, line)
		}
	}
}

func TestFinishedTrackStartsNextQueuedTrack(t *testing.T) {
	source := &testStream{len: 100, position: 100}
	format := beep.Format{SampleRate: 100, NumChannels: 2, Precision: 2}
	m := model{
		playing: track.New(source, &format, "done.mp3", time.Second),
		queue: []queuedTrack{{
			path:  "next.mp3",
			title: "next.mp3",
		}},
	}

	updated, cmd := m.Update(tickMsg(time.Now()))
	got := updated.(model)

	if cmd == nil {
		t.Fatal("finished track with queued item returned nil command, want playback command")
	}
	if len(got.queue) != 0 {
		t.Fatalf("queue length = %d, want 0 after dequeuing next track", len(got.queue))
	}
}
