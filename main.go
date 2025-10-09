package main

import (
  tea "github.com/charmbracelet/bubbletea"
  "github.com/charmbracelet/bubbles/viewport"
)

type model struct {
  message string
  vp      viewport.Model
}

func (m model) Init() tea.Cmd {
  return nil
}

func (m model) View() string {
  return m.vp.View()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
  switch msg := msg.(type) {
  case tea.KeyMsg:
    switch msg.Type {
    case tea.KeyEsc, tea.KeyCtrlC:
      return m, tea.Quit
    }
  }

  return m, nil
}

func main() {
  message := "hello, world"
  vp := viewport.New(0, 2)
  m := model{
    message: message,
    vp: vp,
  }
  m.vp.SetContent(m.message)

  p := tea.NewProgram(m)
  if _, err := p.Run(); err != nil {
    panic(err)
  }
}
