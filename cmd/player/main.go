package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kjloveless/tmp/internal/help"
	"github.com/kjloveless/tmp/internal/track"

  "github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
)

type model struct {
	playing     track.Track
	filepicker  filepicker.Model
	help        help.HelpUI
  err         error
}

type (
	songPlayingMsg string
	errorMsg       error
)

type tickMsg time.Time

func (m *model) playSongCmd(path string) tea.Cmd {
	return func() tea.Msg {
		f, err := os.Open(path)
		if err != nil {
			log.Println("Error opening file:", err)
			return nil
		}
		streamer, format, err := mp3.Decode(f)
		if err != nil {
			log.Println("Error decoding file:", err)
			return nil
		}
		title := filepath.Base(path)
    length := format.SampleRate.D(streamer.Len())
		track := track.New(streamer, &format, title, length)

    resample := beep.Resample(4, format.SampleRate, 48000, track.Control.Streamer)
		speaker.Clear()
		speaker.Play(resample)

		return track
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second/100, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return m.filepicker.Init()
}

func (m model) View() string {
	var builder strings.Builder

	if m.help.GetshowHelp() {
		var b strings.Builder
		b.WriteString("Help — press ? to close\n\n")
		b.WriteString(m.help.ListView())
		return b.String()
	}
	builder.WriteString(m.filepicker.View())
	builder.WriteString("\n")
	statusStyle := lipgloss.NewStyle().Padding(0, 1)
	if m.err != nil {
		builder.WriteString(statusStyle.Render(fmt.Sprintf("❌ Error: %v", m.err)))
	} else if m.playing.Title != "" {
		if m.playing.Control.Paused {
			builder.WriteString(statusStyle.Render(fmt.Sprintf("⏸  Paused: %s", m.playing.Title)))
		} else {
			builder.WriteString(statusStyle.Render(fmt.Sprintf("🎵 Now Playing: %s", m.playing.Title)))
		}
		builder.WriteString(statusStyle.Render(
			"\n" +
				m.playing.String()))
	} else {
		builder.WriteString(statusStyle.Render("Select an MP3 file to play."))
	}
	helpView := m.help.View()
	builder.WriteString("\n" + helpView)
	return builder.String()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.help.Keys().Quit):
			return m, tea.Quit
		case key.Matches(msg, m.help.Keys().PlayPause):
			if m.playing.Title == "" {
				return m, nil
			}
			speaker.Lock()
			m.playing.Control.Paused = !m.playing.Control.Paused
			speaker.Unlock()
			return m, nil

    case key.Matches(msg, m.help.Keys().Loop):
      m.playing.Control.Loop = !m.playing.Control.Loop
      count := 1
      if m.playing.Control.Loop {
        count = -1
      }
      loop := beep.Loop(count, m.playing.Control.Streamer.(beep.StreamSeeker))
      speaker.Play(loop)


		case key.Matches(msg, m.help.Keys().KeyHelp):
			m.help.ToggleShowHelp()
			return m, nil

		}
	case songPlayingMsg:
		m.playing.Title = string(msg)
		m.err = nil
		return m, nil

	case errorMsg:
		m.err = msg
		return m, nil

	case track.Track:
		m.playing = msg
		m.playing.Control.Paused = false
		return m, tickCmd()

	case tickMsg:
		if m.playing.Control.Ctrl != nil && m.playing.Percent() >= 1.0 {
			m.playing.Control.Paused = false
			return m, nil
		}
		if m.playing.Control.Ctrl == nil {
			return m, nil
		}
		return m, tickCmd()

	}
	var cmd tea.Cmd
	m.filepicker, cmd = m.filepicker.Update(msg)

	if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
		if strings.HasSuffix(strings.ToLower(path), ".mp3") {
			return m, m.playSongCmd(path)
		}
	}
	return m, cmd
}

func main() {
	initPath, err := filepath.Abs("./sounds")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := os.Stat(initPath); os.IsNotExist(err) {
		log.Fatalf("Directory does not exist: %s", initPath)
	}
	fp := filepicker.New()
	fp.AllowedTypes = []string{".mp3"}
	fp.CurrentDirectory = initPath

	m := model{
		filepicker: fp,
		help:       help.NewDefault(),
	}

	speaker.Init(48000, 8000)

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}
