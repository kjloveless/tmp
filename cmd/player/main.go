package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
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
	playing          track.Track
	playingPath      string
	queue            []queuedTrack
	queueCursor      int
	filepicker       filepicker.Model
	pickerSelected   int
	pickerStack      []int
	focus            focusMode
	sampleRate       beep.SampleRate
	help             help.HelpUI
	loadingDirectory bool
	width            int
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

func (m model) pickerEntries() ([]os.DirEntry, error) {
	entries, err := os.ReadDir(m.filepicker.CurrentDirectory)
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() == entries[j].IsDir() {
			return entries[i].Name() < entries[j].Name()
		}
		return entries[i].IsDir()
	})

	if m.filepicker.ShowHidden {
		return entries, nil
	}

	visible := entries[:0]
	for _, entry := range entries {
		hidden, _ := filepicker.IsHidden(entry.Name())
		if !hidden {
			visible = append(visible, entry)
		}
	}
	return visible, nil
}

func (m model) canSelectPath(path string) bool {
	if len(m.filepicker.AllowedTypes) == 0 {
		return true
	}

	for _, ext := range m.filepicker.AllowedTypes {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

func isDirectory(entry os.DirEntry, path string) bool {
	info, err := entry.Info()
	if err != nil {
		return entry.IsDir()
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return entry.IsDir()
	}

	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return entry.IsDir()
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		return entry.IsDir()
	}
	return targetInfo.IsDir()
}

func (m model) selectedFilePath() (string, bool) {
	entries, err := m.pickerEntries()
	if err != nil || len(entries) == 0 || m.pickerSelected < 0 || m.pickerSelected >= len(entries) {
		return "", false
	}

	entry := entries[m.pickerSelected]
	path := filepath.Join(m.filepicker.CurrentDirectory, entry.Name())
	if isDirectory(entry, path) || !m.canSelectPath(path) {
		return "", false
	}
	return path, true
}

func (m *model) clampPickerSelected() {
	entries, err := m.pickerEntries()
	if err != nil || len(entries) == 0 {
		m.pickerSelected = 0
		return
	}

	switch {
	case m.pickerSelected < 0:
		m.pickerSelected = 0
	case m.pickerSelected >= len(entries):
		m.pickerSelected = len(entries) - 1
	}
}

func (m *model) syncPickerSelection(msg tea.Msg, previousDirectory string) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		m.clampPickerSelected()
		return
	}

	if m.filepicker.CurrentDirectory != previousDirectory {
		if key.Matches(keyMsg, m.filepicker.KeyMap.Back) {
			if len(m.pickerStack) > 0 {
				last := len(m.pickerStack) - 1
				m.pickerSelected = m.pickerStack[last]
				m.pickerStack = m.pickerStack[:last]
			} else {
				m.pickerSelected = 0
			}
		} else {
			m.pickerStack = append(m.pickerStack, m.pickerSelected)
			m.pickerSelected = 0
		}
		m.clampPickerSelected()
		return
	}

	entries, err := m.pickerEntries()
	if err != nil || len(entries) == 0 {
		m.pickerSelected = 0
		return
	}

	switch {
	case key.Matches(keyMsg, m.filepicker.KeyMap.GoToTop):
		m.pickerSelected = 0
	case key.Matches(keyMsg, m.filepicker.KeyMap.GoToLast):
		m.pickerSelected = len(entries) - 1
	case key.Matches(keyMsg, m.filepicker.KeyMap.Down):
		m.pickerSelected++
	case key.Matches(keyMsg, m.filepicker.KeyMap.Up):
		m.pickerSelected--
	case key.Matches(keyMsg, m.filepicker.KeyMap.PageDown):
		m.pickerSelected += m.filepicker.Height
	case key.Matches(keyMsg, m.filepicker.KeyMap.PageUp):
		m.pickerSelected -= m.filepicker.Height
	}
	m.clampPickerSelected()
}

func (m *model) enqueueSelected() bool {
	path, ok := m.selectedFilePath()
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

	queue := m.queueView()
	leftWidth := m.leftPaneWidth(queue)
	left := lipgloss.NewStyle().
		Width(leftWidth).
		Render(truncateBlock(leftPane.String(), leftWidth))

	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", queuePanelGap), queue)
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

		case key.Matches(msg, m.help.Keys().FocusNext):
			if m.focus == focusQueue {
				m.focus = focusTracks
			} else {
				m.focus = focusQueue
				m.clampQueueCursor()
			}
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

		case key.Matches(msg, m.help.Keys().QueueSelected):
			m.enqueueSelected()
			return m, nil

		case key.Matches(msg, m.help.Keys().DequeueNext):
			if m.focus == focusQueue {
				m.dequeueSelected()
			}
			return m, nil

		case m.focus == focusQueue && msg.Type == tea.KeyDown:
			m.moveQueueCursor(1)
			return m, nil

		case m.focus == focusQueue && msg.Type == tea.KeyUp:
			m.moveQueueCursor(-1)
			return m, nil

		case key.Matches(msg, m.help.Keys().KeyHelp):
			m.help.ToggleShowHelp()
			return m, nil

		}

		if m.focus == focusQueue {
			return m, nil
		}
	case errorMsg:
		m.err = msg
		return m, nil

	case dirLoadedMsg:
		m.loadingDirectory = false
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
	m.syncPickerSelection(msg, prevDir)
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
