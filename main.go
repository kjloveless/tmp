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
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kjloveless/tmp/internal/help"
	"github.com/kjloveless/tmp/internal/track"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
)

type model struct {
	playing    track.Track
	progress   progress.Model
	filepicker filepicker.Model
	err        error
	help       help.HelpUI
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
			log.Println("Error decoding file:", m.err)
			return nil
		}
		title := filepath.Base(path)
		ctrl := &beep.Ctrl{Streamer: streamer, Paused: false}
    speaker.Lock()
    length := format.SampleRate.D(streamer.Len())
    speaker.Unlock()
		track := track.Track{
			Ctrl:   ctrl,
			Format: &format,
			Title:  title,
			Length: length,
		}
		speaker.Clear()
		speaker.Init(
			track.Format.SampleRate,
			track.Format.SampleRate.N(time.Second/10))

		speaker.Play(track.Ctrl)

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
		if m.playing.Ctrl.Paused {
			builder.WriteString(statusStyle.Render(fmt.Sprintf("⏸  Paused: %s", m.playing.Title)))
		} else {
			builder.WriteString(statusStyle.Render(fmt.Sprintf("🎵 Now Playing: %s", m.playing.Title)))
		}
		builder.WriteString(statusStyle.Render(
			"\n" +
				m.progress.ViewAs(m.playing.Percent()) +
				" " +
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
			m.playing.Ctrl.Paused = !m.playing.Ctrl.Paused
			speaker.Unlock()
			return m, nil

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
		m.playing.Ctrl.Paused = false
		return m, tickCmd()

	case tickMsg:
		if m.playing.Ctrl != nil && m.playing.Percent() >= 1.0 {
			m.playing.Ctrl.Paused = false
			return m, nil
		}
		if m.playing.Ctrl == nil {
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
	prog := progress.New(progress.WithScaledGradient("#ff7ccb", "#fdff8c"), progress.WithSpringOptions(6.0, .5))
	prog.ShowPercentage = false

	m := model{
		filepicker: fp,
		progress:   prog,
		help:       help.NewDefault(),
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}
