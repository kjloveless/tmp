package help

import (
	"fmt"
	"log"
	"os"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

type FocusArea string

const (
	FocusTracks FocusArea = "tracks"
	FocusQueue  FocusArea = "queue"
)

type GlobalKeyMap struct {
	PlayPause key.Binding
	FocusNext key.Binding
	Loop      key.Binding
	Quit      key.Binding
	KeyHelp   key.Binding
}

type TracksKeyMap struct {
	QueueSelected key.Binding
}

type QueueKeyMap struct {
	DequeueSelected key.Binding
	Up              key.Binding
	Down            key.Binding
}

type KeyMap struct {
	Global GlobalKeyMap
	Tracks TracksKeyMap
	Queue  QueueKeyMap
}

var DefaultKeyMap = KeyMap{
	Global: GlobalKeyMap{
		PlayPause: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "play/pause"),
		),
		FocusNext: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch focus"),
		),
		Loop: key.NewBinding(
			key.WithKeys("l"),
			key.WithHelp("l", "loop"),
		),
		Quit: key.NewBinding(
			key.WithKeys("esc", "ctrl+c"),
			key.WithHelp("esc/ctrl+c", "quit"),
		),
		KeyHelp: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
	},
	Tracks: TracksKeyMap{
		QueueSelected: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "queue selected"),
		),
	},
	Queue: QueueKeyMap{
		DequeueSelected: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "dequeue selected"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "move down"),
		),
	},
}

type Styles struct {
	Title        lipgloss.Style
	SectionTitle lipgloss.Style
	Key          lipgloss.Style
	Desc         lipgloss.Style
	Row          lipgloss.Style
	Panel        lipgloss.Style
	Separator    string
}

func DefaultStyles() Styles {
	return Styles{
		Title:        lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#89dceb")),
		SectionTitle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#f9e2af")),
		Key:          lipgloss.NewStyle().Foreground(lipgloss.Color("#f5c2e7")).Bold(true),
		Desc:         lipgloss.NewStyle().Foreground(lipgloss.Color("#a6adc8")),
		Row:          lipgloss.NewStyle().Padding(0, 1),
		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#94e2d5")).
			Padding(1, 2),
		Separator: "  —  ",
	}
}

type HelpUI struct {
	model    help.Model
	keys     KeyMap
	showHelp bool
}

func NewHelpUI(keys KeyMap) HelpUI {
	validateOrPanic(keys)
	return HelpUI{
		model: help.New(),
		keys:  keys,
	}
}

func NewDefault() HelpUI {
	return NewHelpUI(DefaultKeyMap)
}

func (hu HelpUI) Keys() KeyMap {
	return hu.keys
}

func (hu HelpUI) GetshowHelp() bool {
	return hu.showHelp
}

func (hu *HelpUI) ToggleShowHelp() {
	hu.showHelp = !hu.showHelp
}

type displayKeyMap struct {
	short []key.Binding
	full  [][]key.Binding
}

func (d displayKeyMap) ShortHelp() []key.Binding  { return d.short }
func (d displayKeyMap) FullHelp() [][]key.Binding { return d.full }

func (hu HelpUI) contextualBindings(focus FocusArea) []key.Binding {
	bindings := []key.Binding{
		hu.keys.Global.PlayPause,
		hu.keys.Global.FocusNext,
		hu.keys.Global.Loop,
		hu.keys.Global.Quit,
		hu.keys.Global.KeyHelp,
	}
	if focus == FocusQueue {
		bindings = append(bindings, hu.keys.Queue.DequeueSelected)
	} else {
		bindings = append(bindings, hu.keys.Tracks.QueueSelected)
	}
	return bindings
}

func (hu HelpUI) View(focus FocusArea) string {
	short := hu.contextualBindings(focus)
	full := make([][]key.Binding, 0, len(short))
	for _, b := range short {
		full = append(full, []key.Binding{b})
	}
	return hu.model.View(displayKeyMap{short: short, full: full})
}

func (hu HelpUI) ViewWithWidth(focus FocusArea, width int) string {
	hu.model.SetWidth(width)
	return hu.View(focus)
}

func (hu HelpUI) ListView(focus FocusArea) string {
	s := DefaultStyles()
	w := hu.model.Width()
	if w <= 0 {
		w = 80
	}

	sectionTitle := func(name string, active bool) string {
		if active {
			name += " (focused)"
		}
		return s.SectionTitle.Render(name)
	}

	renderBindings := func(bindings []key.Binding) string {
		rows := make([]string, 0, len(bindings))
		for _, b := range bindings {
			h := b.Help()
			if h.Key == "" && h.Desc == "" {
				continue
			}
			row := lipgloss.JoinHorizontal(lipgloss.Left, s.Key.Render(h.Key), s.Separator, s.Desc.Render(h.Desc))
			rows = append(rows, s.Row.Render(row))
		}
		return strings.Join(rows, "\n")
	}

	globalBindings := []key.Binding{hu.keys.Global.PlayPause, hu.keys.Global.FocusNext, hu.keys.Global.Loop, hu.keys.Global.Quit, hu.keys.Global.KeyHelp}
	tracksBindings := []key.Binding{hu.keys.Tracks.QueueSelected}
	queueBindings := []key.Binding{hu.keys.Queue.Up, hu.keys.Queue.Down, hu.keys.Queue.DequeueSelected}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		s.Title.Render("Keyboard shortcuts"),
		"",
		sectionTitle("Global controls", false),
		renderBindings(globalBindings),
		"",
		sectionTitle("Tracks controls", focus == FocusTracks),
		renderBindings(tracksBindings),
		"",
		sectionTitle("Queue controls", focus == FocusQueue),
		renderBindings(queueBindings),
	)

	return s.Panel.Width(w).Render(content)
}

func validateOrPanic(keys KeyMap) {
	assertUnique := func(scope string, bindings []key.Binding) {
		seen := make(map[string]string)
		for _, b := range bindings {
			h := b.Help()
			if h.Key == "" {
				continue
			}
			for _, k := range b.Keys() {
				if prev, exists := seen[k]; exists {
					msg := fmt.Sprintf("duplicate hotkey %q in %s: %s and %s", k, scope, prev, h.Desc)
					fmt.Fprintln(os.Stdout, msg)
					log.Print(msg)
					panic(msg)
				}
				seen[k] = h.Desc
			}
		}
	}

	assertUnique("global", []key.Binding{keys.Global.PlayPause, keys.Global.FocusNext, keys.Global.Loop, keys.Global.Quit, keys.Global.KeyHelp})
	assertUnique("tracks", []key.Binding{keys.Tracks.QueueSelected})
	assertUnique("queue", []key.Binding{keys.Queue.Up, keys.Queue.Down, keys.Queue.DequeueSelected})
}
