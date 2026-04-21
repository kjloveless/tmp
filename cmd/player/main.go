package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/kjloveless/tmp/internal/help"
	"github.com/kjloveless/tmp/internal/track"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
)

const (
	defaultWindowWidth     = 80
	defaultWindowHeight    = 24
	queuePanelContentWidth = 36
	minQueuePanelWidth     = 16
	queuePanelGap          = 2
	minLeftPaneWidth       = 20
	minTopPaneHeight       = 4
)

type focusMode int

const (
	focusTracks focusMode = iota
	focusQueue
)

type loopMode int

const (
	loopOff loopMode = iota
	loopCurrent
	loopQueue
)

func (lm loopMode) next() loopMode {
	switch lm {
	case loopOff:
		return loopCurrent
	case loopCurrent:
		return loopQueue
	default:
		return loopOff
	}
}

func (lm loopMode) String() string {
	switch lm {
	case loopCurrent:
		return "current"
	case loopQueue:
		return "queue"
	default:
		return "off"
	}
}

type model struct {
	playing     track.Track
	playingPath string
	queue       []queuedTrack
	queueCursor int
	tracks      tracksComponent
	focus       focusMode
	loopMode    loopMode
	sampleRate  beep.SampleRate
	help        help.HelpUI
	width       int
	height      int
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

	m.playing.Control.Loop = m.loopMode == loopCurrent

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

func (m *model) toggleLoopMode() error {
	previous := m.loopMode
	m.loopMode = m.loopMode.next()
	if err := m.updatePlaybackLoop(); err != nil {
		m.loopMode = previous
		_ = m.updatePlaybackLoop()
		return err
	}
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

func (m *model) enqueueTrack(path, title string) bool {
	if path == "" {
		return false
	}
	if title == "" {
		title = filepath.Base(path)
	}

	m.queue = append(m.queue, queuedTrack{
		path:  path,
		title: title,
	})
	return true
}

func (m *model) enqueueSelected() bool {
	path, ok := m.tracks.selectedFilePath()
	if !ok {
		return false
	}

	return m.enqueueTrack(path, filepath.Base(path))
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

func (m *model) ensureFocusablePane() {
	if m.focus == focusQueue && len(m.queue) == 0 {
		m.focus = focusTracks
	}
}

func (m *model) focusNextPane() {
	if m.focus == focusQueue {
		m.focus = focusTracks
		return
	}

	if len(m.queue) == 0 {
		m.focus = focusTracks
		return
	}

	m.focus = focusQueue
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
	m.ensureFocusablePane()
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
	m.ensureFocusablePane()
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

func boundedWidth(width int) int {
	if width < 0 {
		return 0
	}
	return width
}

func trackPanelStyle(focused bool) lipgloss.Style {
	borderColor := lipgloss.Color("#89dceb")
	if focused {
		borderColor = lipgloss.Color("#f5c2e7")
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)
}

func queuePanelStyle(focused bool, width int) lipgloss.Style {
	borderColor := lipgloss.Color("#89dceb")
	if focused {
		borderColor = lipgloss.Color("#f5c2e7")
	}

	return lipgloss.NewStyle().
		Width(boundedWidth(width)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)
}

func playerHelpPanelStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#94e2d5")).
		Padding(0, 1)
}

func (m *model) queueView() string {
	return m.queueViewWithWidth(queuePanelContentWidth)
}

func queueLine(width int, s string) string {
	return ansi.Truncate(s, boundedWidth(width), "")
}

func (m *model) queueViewWithWidth(width int) string {
	queueStyle := queuePanelStyle(m.focus == focusQueue, width)
	contentWidth := boundedWidth(width - queueStyle.GetHorizontalFrameSize())

	lines := []string{queueLine(contentWidth, "Queue")}
	if m.playing.Title != "" {
		lines = append(lines,
			"",
			queueLine(contentWidth, "Playing"),
			queueLine(contentWidth, "  "+m.playing.Title),
		)
	}

	lines = append(lines, "", queueLine(contentWidth, "Up Next"))
	if len(m.queue) == 0 {
		lines = append(lines, queueLine(contentWidth, "  (empty)"))
	} else {
		for i, item := range m.queue {
			prefix := "  "
			if m.focus == focusQueue && i == m.queueCursor {
				prefix = "> "
			}
			lines = append(lines, queueLine(contentWidth, fmt.Sprintf("%s%d. %s", prefix, i+1, item.title)))
		}
	}

	return queueStyle.Render(strings.Join(lines, "\n"))
}

func (m model) windowWidth() int {
	if m.width > 0 {
		return m.width
	}
	return defaultWindowWidth
}

func (m model) windowHeight() int {
	if m.height > 0 {
		return m.height
	}
	return defaultWindowHeight
}

type topPaneSizing struct {
	leftWidth  int
	queueWidth int
	gap        int
}

func (m model) topPaneSizing() topPaneSizing {
	gap := queuePanelGap
	if m.windowWidth() < gap {
		gap = boundedWidth(m.windowWidth())
	}

	availableWidth := boundedWidth(m.windowWidth() - gap)
	queueWidth := queuePanelContentWidth
	if availableWidth < minLeftPaneWidth+queueWidth {
		queueWidth = availableWidth - minLeftPaneWidth
	}
	if queueWidth < minQueuePanelWidth {
		queueWidth = minQueuePanelWidth
	}
	if queueWidth > availableWidth {
		queueWidth = availableWidth
	}

	return topPaneSizing{
		leftWidth:  availableWidth - queueWidth,
		queueWidth: queueWidth,
		gap:        gap,
	}
}

func (m model) playerHelpPanelWidth() int {
	width := m.windowWidth()
	if width < minLeftPaneWidth {
		return minLeftPaneWidth
	}
	return width
}

func (m model) playerHelpContentWidth() int {
	width := m.playerHelpPanelWidth() - playerHelpPanelStyle().GetHorizontalFrameSize()
	if width < minLeftPaneWidth {
		return minLeftPaneWidth
	}
	return width
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

func truncateBlockHeight(s string, height int) string {
	if height <= 0 {
		return ""
	}

	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
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

func (m model) playerHelpView() string {
	contentWidth := m.playerHelpContentWidth()
	statusStyle := lipgloss.NewStyle().Padding(0, 1).MaxWidth(contentWidth)

	lines := make([]string, 0, 4)
	if m.err != nil {
		lines = append(lines, statusStyle.Render(fmt.Sprintf("❌ Error: %v", m.err)))
	} else if m.playing.Title != "" {
		statusText := fmt.Sprintf("🎵 Now Playing: %s", m.playing.Title)
		if m.playing.Control.Paused {
			statusText = fmt.Sprintf("⏸  Paused: %s", m.playing.Title)
		}
		if m.loopMode != loopOff {
			statusText += fmt.Sprintf("  🔁 Loop: %s", m.loopMode)
		}
		lines = append(lines, statusStyle.Render(statusText))
		lines = append(lines, statusStyle.Render(m.playing.String()))
	} else {
		statusText := "Select an MP3 file to play."
		if m.loopMode != loopOff {
			statusText += fmt.Sprintf("  🔁 Loop: %s", m.loopMode)
		}
		lines = append(lines, statusStyle.Render(statusText))
	}

	if helpView := m.help.ViewWithWidth(m.helpFocus(), contentWidth); helpView != "" {
		lines = append(lines, helpView)
	}

	return playerHelpPanelStyle().
		Width(m.playerHelpPanelWidth()).
		Render(truncateBlock(strings.Join(lines, "\n"), contentWidth))
}

func (m model) topPaneHeight(bottom string) int {
	height := m.windowHeight() - lipgloss.Height(bottom)
	if height < minTopPaneHeight {
		return minTopPaneHeight
	}
	return height
}

func (m model) tracksViewHeight(topHeight int) int {
	height := topHeight - trackPanelStyle(m.focus == focusTracks).GetVerticalFrameSize() - 1
	if height < 0 {
		return 0
	}
	return height
}

func (m model) render() string {
	if m.help.GetshowHelp() {
		var b strings.Builder
		b.WriteString("Help — press ? to close\n\n")
		b.WriteString(m.help.ListView(m.helpFocus()))
		return b.String()
	}
	bottom := m.playerHelpView()
	topHeight := m.topPaneHeight(bottom)

	sizing := m.topPaneSizing()
	queue := m.queueViewWithWidth(sizing.queueWidth)
	trackStyle := trackPanelStyle(m.focus == focusTracks)
	leftContentWidth := boundedWidth(sizing.leftWidth - trackStyle.GetHorizontalFrameSize())
	var leftPane strings.Builder
	leftPane.WriteString(m.tracks.ViewWithHeight(m.tracksViewHeight(topHeight)))
	left := trackStyle.
		Width(sizing.leftWidth).
		Render(truncateBlock(leftPane.String(), leftContentWidth))

	top := lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", sizing.gap), queue)
	top = truncateBlock(top, m.windowWidth())
	top = truncateBlockHeight(top, topHeight)
	return lipgloss.JoinVertical(lipgloss.Left, top, bottom)
}

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
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
			m.focusNextPane()
			return m, nil

		case key.Matches(msg, m.help.Keys().Global.Loop):
			if err := m.toggleLoopMode(); err != nil {
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
		m.height = msg.Height

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

		if err := m.updatePlaybackLoop(); err != nil {
			m.err = err
			return m, nil
		}

		resample := beep.Resample(4, m.playing.Format.SampleRate, m.sampleRate, m.playing.Control.Ctrl)
		speaker.Play(resample)

		return m, tickCmd()

	case tickMsg:
		if !m.isPlaying() {
			return m, nil
		}
		if m.loopMode != loopCurrent && m.playing.Percent() >= 1.0 {
			if m.loopMode == loopQueue {
				m.enqueueTrack(m.playingPath, m.playing.Title)
			}
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

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}
