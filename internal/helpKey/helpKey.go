package helpKey

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	PlayPause key.Binding
	Quit      key.Binding
}

func InitKeyHelp() KeyMap {
	return KeyMap{
		PlayPause: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "play/pause"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q/ctrl+c", "quit"),
		),
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.PlayPause, k.Quit}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.PlayPause},
		{k.Quit},
	}
}
