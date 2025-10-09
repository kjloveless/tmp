package main

import (
  "log"
  "os"
  "time"

  tea "github.com/charmbracelet/bubbletea"
  "github.com/charmbracelet/bubbles/viewport"

  "github.com/gopxl/beep/mp3"
  "github.com/gopxl/beep/speaker"
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

  f, err := os.Open("break.mp3")
  if err != nil {
    log.Fatal(err)
  }

  streamer, format, err := mp3.Decode(f)
  if err != nil {
    log.Fatal(err)
  }
  defer streamer.Close()

  speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
  speaker.Play(streamer)

  p := tea.NewProgram(m)
  if _, err := p.Run(); err != nil {
    panic(err)
  }
}
