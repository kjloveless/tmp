package help

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

var DefaultKeyMap = KeyMap{
	PlayPause: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "resume/pause"),
	),
  Loop: key.NewBinding(
    key.WithKeys("l"),
    key.WithHelp("l", "loop"),
  ),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q/ctrl+c", "quit"),
	),

	KeyHelp: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
}

type Styles struct {
	Title     lipgloss.Style
	Key       lipgloss.Style
	Desc      lipgloss.Style
	Row       lipgloss.Style
	Panel     lipgloss.Style
	Separator string
}

func DefaultStyles() Styles {
	return Styles{
		Title: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#89dceb")),
		Key:   lipgloss.NewStyle().Foreground(lipgloss.Color("#f5c2e7")).Bold(true),
		Desc:  lipgloss.NewStyle().Foreground(lipgloss.Color("#a6adc8")),
		Row:   lipgloss.NewStyle().Padding(0, 1),
		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#94e2d5")).
			Padding(1, 2),
		Separator: "  —  ",
	}
}

func (hu HelpUI) listEntries(s Styles) []string {
	var out []string
	for _, group := range hu.keys.FullHelp() {
		for _, b := range group {
			h := b.Help()
			k := h.Key
			d := h.Desc
			if k == "" && d == "" {
				continue
			}
			row := lipgloss.JoinHorizontal(
				lipgloss.Left,
				s.Key.Render(k),
				s.Separator,
				s.Desc.Render(d),
			)
			out = append(out, s.Row.Render(row))
		}
	}
	return out
}

func (hu HelpUI) ListView() string {
	s := DefaultStyles()

	entries := hu.listEntries(s)
	if len(entries) == 0 {
		return ""
	}

	w := hu.model.Width
	if w <= 0 {
		w = 80
	}

	half := (len(entries) + 1) / 2
	left := entries[:half]
	right := entries[half:]

	colGap := 4
	colWidth := (w - colGap - 2*s.Panel.GetHorizontalBorderSize() - 2*s.Panel.GetHorizontalPadding()) / 2
	if colWidth < 20 {
		body := lipgloss.NewStyle().Width(w).Render(strings.Join(entries, "\n"))
		content := lipgloss.JoinVertical(lipgloss.Left, s.Title.Render("Keyboard shortcuts"), body)
		return s.Panel.Width(w).Render(content)
	}

	leftCol := lipgloss.NewStyle().Width(colWidth).Render(strings.Join(left, "\n"))
	rightCol := lipgloss.NewStyle().Width(colWidth).Render(strings.Join(right, "\n"))
	rows := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, strings.Repeat(" ", colGap), rightCol)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		s.Title.Render("Keyboard shortcuts"),
		rows,
	)

	return s.Panel.Width(w).Render(content)
}

func (hu HelpUI) View() string {
	return hu.model.View(hu.keys)
}

func (hu HelpUI) Keys() KeyMap {
	return hu.keys
}

type KeyMap struct {
	PlayPause key.Binding
  Loop      key.Binding
	Quit      key.Binding
	KeyHelp   key.Binding
}
type HelpUI struct {
	model    help.Model
	keys     KeyMap
	showHelp bool
}

func NewHelpUI(keys KeyMap) HelpUI {
	return HelpUI{
		model: help.New(),
		keys:  keys,
	}
}

func (hu HelpUI) GetshowHelp() bool {
	return hu.showHelp
}

func (hu *HelpUI) ToggleShowHelp() {
	hu.showHelp = !hu.showHelp
}

func NewDefault() HelpUI {
	return NewHelpUI(DefaultKeyMap)
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.PlayPause, k.Loop, k.Quit, k.KeyHelp}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.PlayPause},
    {k.Loop},
		{k.Quit},
		{k.KeyHelp},
	}
}
