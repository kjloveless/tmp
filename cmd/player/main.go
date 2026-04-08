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
	playing          track.Track
	filepicker       filepicker.Model
	sampleRate       beep.SampleRate
	help             help.HelpUI
	loadingDirectory bool
	err              error
}

type (
	errorMsg       error
	loadedTrackMsg struct {
		track    track.Track
		previous beep.StreamSeekCloser
	}
)

type tickMsg time.Time
type dirLoadedMsg struct{}

func (m *model) updatePlaybackLoop() error {
	if m.playing.Control.Ctrl == nil || m.playing.Control.Source == nil {
		return nil
	}

	streamer := beep.Streamer(m.playing.Control.Source)
	if m.playing.Control.Loop {
		looped, err := beep.Loop2(m.playing.Control.Source)
		if err != nil {
			return err
		}
		streamer = looped
	}

	speaker.Lock()
	m.playing.Control.Streamer = streamer
	speaker.Unlock()

	return nil
}

func (m *model) playSongCmd(path string) tea.Cmd {
	previous := m.playing.Control.Source

	return func() tea.Msg {
		f, err := os.Open(path)
		if err != nil {
			return errorMsg(fmt.Errorf("open %s: %w", filepath.Base(path), err))
		}
		streamer, format, err := mp3.Decode(f)
		if err != nil {
			_ = f.Close()
			return errorMsg(fmt.Errorf("decode %s: %w", filepath.Base(path), err))
		}
		title := filepath.Base(path)
		length := format.SampleRate.D(streamer.Len())
		return loadedTrackMsg{
			track:    track.New(streamer, &format, title, length),
			previous: previous,
		}
	}
}

func (m *model) stopPlayback() error {
	if m.playing.Control.Source == nil {
		return nil
	}

	speaker.Clear()
	err := m.playing.Control.Source.Close()
	m.playing = track.Track{}
	return err
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
		statusText := fmt.Sprintf("🎵 Now Playing: %s", m.playing.Title)
		if m.playing.Control.Paused {
			statusText = fmt.Sprintf("⏸  Paused: %s", m.playing.Title)
		}
		if m.playing.Control.Loop {
			statusText += "  🔁 Loop On"
		}
		builder.WriteString(statusStyle.Render(statusText))
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
			if err := m.stopPlayback(); err != nil {
				log.Printf("error closing active track: %v", err)
			}
			return m, tea.Quit
		case key.Matches(msg, m.help.Keys().PlayPause):
			if m.playing.Control.Ctrl == nil {
				return m, nil
			}
			speaker.Lock()
			m.playing.Control.Paused = !m.playing.Control.Paused
			speaker.Unlock()
			return m, nil

		case key.Matches(msg, m.help.Keys().Loop):
			if m.playing.Control.Ctrl == nil {
				return m, nil
			}

			m.playing.Control.Loop = !m.playing.Control.Loop
			if err := m.updatePlaybackLoop(); err != nil {
				m.playing.Control.Loop = !m.playing.Control.Loop
				m.err = err
				return m, nil
			}

			m.err = nil
			return m, nil

		case key.Matches(msg, m.help.Keys().KeyHelp):
			m.help.ToggleShowHelp()
			return m, nil

		}
	case errorMsg:
		m.err = msg
		return m, nil

	case dirLoadedMsg:
		m.loadingDirectory = false
		return m, nil

	case loadedTrackMsg:
		speaker.Clear()
		if msg.previous != nil {
			if err := msg.previous.Close(); err != nil {
				log.Printf("error closing previous track: %v", err)
			}
		}

		m.playing = msg.track
		m.playing.Control.Paused = false
		m.err = nil

		resample := beep.Resample(4, m.playing.Format.SampleRate, m.sampleRate, m.playing.Control.Ctrl)
		speaker.Play(resample)

		return m, tickCmd()

	case tickMsg:
		if m.playing.Control.Ctrl == nil || m.playing.Control.Source == nil {
			return m, nil
		}
		if !m.playing.Control.Loop && m.playing.Percent() >= 1.0 {
			m.playing.Control.Paused = false
			return m, nil
		}
		return m, tickCmd()

	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok && m.loadingDirectory {
		if key.Matches(keyMsg, m.filepicker.KeyMap.Open) ||
			key.Matches(keyMsg, m.filepicker.KeyMap.Select) ||
			key.Matches(keyMsg, m.filepicker.KeyMap.Back) {
			return m, nil
		}
	}

	prevDir := m.filepicker.CurrentDirectory
	var cmd tea.Cmd
	m.filepicker, cmd = m.filepicker.Update(msg)
	if m.filepicker.CurrentDirectory != prevDir && cmd != nil {
		m.loadingDirectory = true
		cmd = tea.Sequence(cmd, func() tea.Msg { return dirLoadedMsg{} })
	}

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

	var sr beep.SampleRate = 48000

	m := model{
		filepicker: fp,
		sampleRate: sr,
		help:       help.NewDefault(),
	}

	speaker.Init(m.sampleRate, m.sampleRate.N(time.Second/10))

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}
