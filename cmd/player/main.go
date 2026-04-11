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
	playingPath      string
	queue            []queuedTrack
	filepicker       filepicker.Model
	sampleRate       beep.SampleRate
	help             help.HelpUI
	loadingDirectory bool
	err              error
}

type queuedTrack struct {
	path  string
	title string
}

type (
	errorMsg       error
	loadedTrackMsg struct {
		track    track.Track
		path     string
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
			path:     path,
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
	m.playingPath = ""
	return err
}

func (m *model) enqueueCurrent() {
	if m.playing.Title == "" || m.playingPath == "" {
		return
	}

	m.queue = append(m.queue, queuedTrack{
		path:  m.playingPath,
		title: m.playing.Title,
	})
}

func (m *model) dequeueNext() (queuedTrack, bool) {
	if len(m.queue) == 0 {
		return queuedTrack{}, false
	}

	next := m.queue[0]
	m.queue = m.queue[1:]
	return next, true
}

func (m *model) queueView() string {
	queueStyle := lipgloss.NewStyle().
		Width(36).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#89dceb")).
		Padding(0, 1)

	lines := []string{"Up Next"}
	if len(m.queue) == 0 {
		lines = append(lines, "  (queue is empty)")
	} else {
		for i, item := range m.queue {
			lines = append(lines, fmt.Sprintf("  %d. %s", i+1, item.title))
		}
	}

	return queueStyle.Render(strings.Join(lines, "\n"))
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
	if m.help.GetshowHelp() {
		var b strings.Builder
		b.WriteString("Help — press ? to close\n\n")
		b.WriteString(m.help.ListView())
		return b.String()
	}
	var leftPane strings.Builder
	leftPane.WriteString(m.filepicker.View())
	leftPane.WriteString("\n")
	statusStyle := lipgloss.NewStyle().Padding(0, 1)
	if m.err != nil {
		leftPane.WriteString(statusStyle.Render(fmt.Sprintf("❌ Error: %v", m.err)))
	} else if m.playing.Title != "" {
		statusText := fmt.Sprintf("🎵 Now Playing: %s", m.playing.Title)
		if m.playing.Control.Paused {
			statusText = fmt.Sprintf("⏸  Paused: %s", m.playing.Title)
		}
		if m.playing.Control.Loop {
			statusText += "  🔁 Loop On"
		}
		leftPane.WriteString(statusStyle.Render(statusText))
		leftPane.WriteString(statusStyle.Render(
			"\n" +
				m.playing.String()))
	} else {
		leftPane.WriteString(statusStyle.Render("Select an MP3 file to play."))
	}
	helpView := m.help.View()
	leftPane.WriteString("\n" + helpView)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane.String(), "  ", m.queueView())
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

		case key.Matches(msg, m.help.Keys().QueueCurrent):
			m.enqueueCurrent()
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
		m.playingPath = msg.path
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
			if next, ok := m.dequeueNext(); ok {
				return m, m.playSongCmd(next.path)
			}

			if err := m.stopPlayback(); err != nil {
				m.err = err
			}
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
