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

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
)

type track struct {
	ctrl   *beep.Ctrl
	format *beep.Format
	title  string
	length time.Duration
}

func (t track) Position() time.Duration {
  speaker.Lock()
	if streamer, ok := t.ctrl.Streamer.(beep.StreamSeeker); ok {
    duration := t.format.SampleRate.D(streamer.Position())
		speaker.Unlock()
    return duration
	}
  speaker.Unlock()
	panic("failure to retrieve position from track")
}

func (t track) Percent() float64 {
	return t.Position().Seconds() / t.length.Seconds()
}

func (t track) String() string {
	return fmt.Sprintf(
		"%s : %s",
		t.Position().Round(time.Second),
		t.length.Round(time.Second))
}

type model struct {
	playing    track
	pause      bool
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
		track := track{
			ctrl:   ctrl,
			format: &format,
			title:  title,
			length: length,
		}
		speaker.Clear()
		speaker.Init(
			track.format.SampleRate,
			track.format.SampleRate.N(time.Second/10))

		speaker.Play(track.ctrl)

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
	} else if m.playing.title != "" {
		if m.pause {
			builder.WriteString(statusStyle.Render(fmt.Sprintf("⏸  Paused: %s", m.playing.title)))
		} else {
			builder.WriteString(statusStyle.Render(fmt.Sprintf("🎵 Now Playing: %s", m.playing.title)))
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
			if m.playing.title == "" {
				return m, nil
			}
			speaker.Lock()
			m.playing.ctrl.Paused = !m.playing.ctrl.Paused
			speaker.Unlock()
			m.pause = m.playing.ctrl.Paused
			return m, nil

		case key.Matches(msg, m.help.Keys().KeyHelp):
			m.help.ToggleShowHelp()
			return m, nil

		}
	case songPlayingMsg:
		m.playing.title = string(msg)
		m.err = nil
		return m, nil

	case errorMsg:
		m.err = msg
		return m, nil

	case track:
		m.playing = msg
		m.pause = false
		return m, tickCmd()

	case tickMsg:
		if m.playing.ctrl != nil && m.playing.Percent() >= 1.0 {
			m.pause = false
			return m, nil
		}
		if m.playing.ctrl == nil {
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
