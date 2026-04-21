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
	"github.com/charmbracelet/x/ansi"
	"github.com/kjloveless/tmp/internal/help"
	"github.com/kjloveless/tmp/internal/track"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
)

const (
	defaultWindowWidth     = 80
	queuePanelContentWidth = 36
	queuePanelGap          = 2
	minLeftPaneWidth       = 20
)

type focusMode int

const (
	focusTracks focusMode = iota
	focusQueue
)

type model struct {
	playing     track.Track
	playingPath string
	queue       []queuedTrack
	queueCursor int
	tracks      tracksComponent
	focus       focusMode
	sampleRate  beep.SampleRate
	help        help.HelpUI
	width       int
	err         error
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

func (m *model) enqueueSelected() bool {
	path, ok := m.tracks.selectedFilePath()
	if !ok {
		return false
	}

	m.queue = append(m.queue, queuedTrack{
		path:  path,
		title: filepath.Base(path),
	})
	return true
}

func (m *model) clampQueueCursor() {
	if len(m.queue) == 0 {
		m.queueCursor = 0
		return
	}

	switch {
	case m.queueCursor < 0:
		m.queueCursor = 0
	case m.queueCursor >= len(m.queue):
		m.queueCursor = len(m.queue) - 1
	}
}

func (m *model) moveQueueCursor(delta int) {
	m.queueCursor += delta
	m.clampQueueCursor()
}

func (m *model) dequeueSelected() (queuedTrack, bool) {
	if len(m.queue) == 0 {
		return queuedTrack{}, false
	}

	m.clampQueueCursor()
	selected := m.queue[m.queueCursor]
	m.queue = append(m.queue[:m.queueCursor], m.queue[m.queueCursor+1:]...)
	m.clampQueueCursor()
	return selected, true
}

func (m *model) dequeueNext() (queuedTrack, bool) {
	if len(m.queue) == 0 {
		return queuedTrack{}, false
	}

	next := m.queue[0]
	m.queue = m.queue[1:]
	if m.queueCursor > 0 {
		m.queueCursor--
	}
	m.clampQueueCursor()
	return next, true
}

func (m *model) playNextQueuedCmd() (tea.Cmd, bool) {
	next, ok := m.dequeueNext()
	if !ok {
		return nil, false
	}
	return m.playSongCmd(next.path), true
}

func (m model) isPlaying() bool {
	return m.playing.Control.Ctrl != nil && m.playing.Control.Source != nil
}

func queueLine(s string) string {
	return lipgloss.NewStyle().MaxWidth(queuePanelContentWidth).Render(s)
}

func (m *model) queueView() string {
	borderColor := lipgloss.Color("#89dceb")
	if m.focus == focusQueue {
		borderColor = lipgloss.Color("#f5c2e7")
	}

	queueStyle := lipgloss.NewStyle().
		Width(queuePanelContentWidth).
		MaxWidth(queuePanelContentWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)

	lines := []string{"Queue"}
	if m.playing.Title != "" {
		lines = append(lines,
			"",
			"Playing",
			queueLine("  "+m.playing.Title),
		)
	}

	lines = append(lines, "", "Up Next")
	if len(m.queue) == 0 {
		lines = append(lines, "  (empty)")
	} else {
		for i, item := range m.queue {
			prefix := "  "
			if m.focus == focusQueue && i == m.queueCursor {
				prefix = "> "
			}
			lines = append(lines, queueLine(fmt.Sprintf("%s%d. %s", prefix, i+1, item.title)))
		}
	}

	return queueStyle.Render(strings.Join(lines, "\n"))
}

func (m model) leftPaneWidth(queue string) int {
	width := m.width
	if width <= 0 {
		width = defaultWindowWidth
	}

	leftWidth := width - lipgloss.Width(queue) - queuePanelGap
	if leftWidth < minLeftPaneWidth {
		return minLeftPaneWidth
	}
	return leftWidth
}

func truncateBlock(s string, width int) string {
	if width <= 0 {
		return ""
	}

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "")
	}
	return strings.Join(lines, "\n")
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second/100, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return m.tracks.Init()
}

func (m model) helpFocus() help.FocusArea {
	if m.focus == focusQueue {
		return help.FocusQueue
	}
	return help.FocusTracks
}

func (m model) View() string {
	if m.help.GetshowHelp() {
		var b strings.Builder
		b.WriteString("Help — press ? to close\n\n")
		b.WriteString(m.help.ListView(m.helpFocus()))
		return b.String()
	}
	var leftPane strings.Builder
	leftPane.WriteString(m.tracks.View())
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
	helpView := m.help.View(m.helpFocus())
	leftPane.WriteString("\n" + helpView)

	queue := m.queueView()
	leftWidth := m.leftPaneWidth(queue)
	leftBorderColor := lipgloss.Color("#89dceb")
	if m.focus == focusTracks {
		leftBorderColor = lipgloss.Color("#f5c2e7")
	}
	left := lipgloss.NewStyle().
		Width(leftWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(leftBorderColor).
		Padding(0, 1).
		Render(truncateBlock(leftPane.String(), leftWidth))

	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", queuePanelGap), queue)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Global control board hotkeys are always handled first.
		switch {
		case key.Matches(msg, m.help.Keys().Global.Quit):
			if err := m.stopPlayback(); err != nil {
				log.Printf("error closing active track: %v", err)
			}
			return m, tea.Quit
		case key.Matches(msg, m.help.Keys().Global.PlayPause):
			if !m.isPlaying() {
				if len(m.queue) == 0 {
					m.enqueueSelected()
				}
				if cmd, ok := m.playNextQueuedCmd(); ok {
					return m, cmd
				}
				return m, nil
			}
			speaker.Lock()
			m.playing.Control.Paused = !m.playing.Control.Paused
			speaker.Unlock()
			return m, nil

		case key.Matches(msg, m.help.Keys().Global.FocusNext):
			if m.focus == focusQueue {
				m.focus = focusTracks
			} else {
				m.focus = focusQueue
				m.clampQueueCursor()
			}
			return m, nil

		case key.Matches(msg, m.help.Keys().Global.Loop):
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

		case key.Matches(msg, m.help.Keys().Global.KeyHelp):
			m.help.ToggleShowHelp()
			return m, nil
		}

		// Focused component hotkeys are handled after globals.
		switch m.focus {
		case focusQueue:
			switch {
			case key.Matches(msg, m.help.Keys().Queue.DequeueSelected):
				m.dequeueSelected()
				return m, nil
			case key.Matches(msg, m.help.Keys().Queue.Down):
				m.moveQueueCursor(1)
				return m, nil
			case key.Matches(msg, m.help.Keys().Queue.Up):
				m.moveQueueCursor(-1)
				return m, nil
			default:
				// Ignore unbound keys while queue is focused.
				return m, nil
			}
		case focusTracks:
			if key.Matches(msg, m.help.Keys().Tracks.QueueSelected) {
				m.enqueueSelected()
				return m, nil
			}
		}

		if m.focus == focusQueue {
			return m, nil
		}
	case errorMsg:
		m.err = msg
		return m, nil

	case dirLoadedMsg:
		m.tracks.loadingDirectory = false
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width

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
		if !m.isPlaying() {
			return m, nil
		}
		if !m.playing.Control.Loop && m.playing.Percent() >= 1.0 {
			if cmd, ok := m.playNextQueuedCmd(); ok {
				return m, cmd
			}

			if err := m.stopPlayback(); err != nil {
				m.err = err
			}
			return m, nil
		}
		return m, tickCmd()

	}

	cmd, path, didSelect := m.tracks.Update(msg)
	if didSelect {
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
		tracks:     newTracksComponent(fp),
		sampleRate: sr,
		help:       help.NewDefault(),
	}

	speaker.Init(m.sampleRate, m.sampleRate.N(time.Second/10))

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}
