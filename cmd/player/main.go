package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"math/cmplx"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/kjloveless/tmp/internal/help"
	"github.com/kjloveless/tmp/internal/track"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/gopxl/beep/v2/wav"
)

const (
	defaultWindowWidth     = 80
	defaultWindowHeight    = 24
	queuePanelContentWidth = 36
	minQueuePanelWidth     = 16
	queuePanelGap          = 2
	rightPaneGap           = 1
	minLeftPaneWidth       = 20
	minTopPaneHeight       = 4
	visualizerWidthRatio   = 0.55
	minVisualizerWidth     = 24
	maxVisualizerWidth     = 52
	minSpectrogramHeight   = 6
	maxSpectrogramHeight   = 10
	minQueueContentHeight  = 4
	spectrumFFTSize        = 1024
	spectrumHistorySize    = 160
	spectrumMinFreq        = 32.0
	spectrumMaxFreq        = 18000.0
	spectrumFloorDB        = -72.0
	seekStep               = 5 * time.Second
	volumeStep             = 10
	maxVolumePercent       = 150
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
	playing       track.Track
	playingPath   string
	queue         []queuedTrack
	queueCursor   int
	tracks        tracksComponent
	focus         focusMode
	loopMode      loopMode
	sampleRate    beep.SampleRate
	help          help.HelpUI
	width         int
	height        int
	volume        int
	muted         bool
	transitioning bool
	meter         *audioMeter
	err           error
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

type audioMeter struct {
	mu         sync.RWMutex
	bins       []float64
	history    []float64
	frames     [][]float64
	sampleRate float64
}

type namedBandLevel struct {
	Label string
	Value float64
}

func supportedAudioExtensions() []string {
	return []string{".mp3", ".wav"}
}

func isSupportedAudioFile(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range supportedAudioExtensions() {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func newAudioMeter(binCount int) *audioMeter {
	if binCount < 8 {
		binCount = 8
	}
	return &audioMeter{
		bins:       make([]float64, binCount),
		history:    make([]float64, 0, spectrumFFTSize),
		frames:     make([][]float64, 0, spectrumHistorySize),
		sampleRate: 48000,
	}
}

func (m *audioMeter) Reset() {
	m.mu.Lock()
	for i := range m.bins {
		m.bins[i] = 0
	}
	m.history = m.history[:0]
	m.frames = m.frames[:0]
	m.mu.Unlock()
}

func (m *audioMeter) SetSampleRate(sampleRate beep.SampleRate) {
	if sampleRate <= 0 {
		return
	}

	m.mu.Lock()
	m.sampleRate = float64(sampleRate)
	m.mu.Unlock()
}

func (m *audioMeter) Process(samples [][2]float64) {
	if len(samples) == 0 {
		return
	}

	mono := make([]float64, len(samples))
	for i, sample := range samples {
		mono[i] = (sample[0] + sample[1]) / 2
	}

	m.mu.Lock()
	m.history = append(m.history, mono...)
	if len(m.history) > spectrumFFTSize {
		m.history = append(m.history[:0], m.history[len(m.history)-spectrumFFTSize:]...)
	}

	if len(m.history) < spectrumFFTSize {
		m.mu.Unlock()
		return
	}

	window := append([]float64(nil), m.history...)
	sampleRate := m.sampleRate
	binCount := len(m.bins)
	m.mu.Unlock()

	spectrum := analyzeSpectrum(window, sampleRate, binCount)

	m.mu.Lock()
	for i, v := range spectrum {
		decay := m.bins[i] * 0.88
		if v > decay {
			m.bins[i] = v
		} else {
			m.bins[i] = decay
		}
	}
	m.frames = append(m.frames, append([]float64(nil), spectrum...))
	if len(m.frames) > spectrumHistorySize {
		m.frames = append([][]float64(nil), m.frames[len(m.frames)-spectrumHistorySize:]...)
	}
	m.mu.Unlock()
}

func (m *audioMeter) Bins(width int) []float64 {
	if width <= 0 {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.bins) == 0 {
		return make([]float64, width)
	}

	out := make([]float64, width)
	for i := 0; i < width; i++ {
		src := int(float64(i) * float64(len(m.bins)) / float64(width))
		if src >= len(m.bins) {
			src = len(m.bins) - 1
		}
		out[i] = m.bins[src]
	}
	return out
}

func (m *audioMeter) Spectrogram(width, height int) [][]float64 {
	if width <= 0 || height <= 0 {
		return nil
	}

	grid := make([][]float64, height)
	for i := range grid {
		grid[i] = make([]float64, width)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.frames) == 0 || len(m.bins) == 0 {
		return grid
	}

	bandCount := len(m.bins)
	fillColumn := func(column int, frames [][]float64) {
		for row := 0; row < height; row++ {
			bandStart := (height - row - 1) * bandCount / height
			bandEnd := (height - row) * bandCount / height
			if bandEnd <= bandStart {
				bandEnd = bandStart + 1
			}
			if bandEnd > bandCount {
				bandEnd = bandCount
			}

			var peak float64
			for _, frame := range frames {
				for i := bandStart; i < bandEnd; i++ {
					if frame[i] > peak {
						peak = frame[i]
					}
				}
			}
			grid[row][column] = peak
		}
	}

	frameCount := len(m.frames)
	if frameCount <= width {
		offset := width - frameCount
		for i, frame := range m.frames {
			fillColumn(offset+i, [][]float64{frame})
		}
		return grid
	}

	for column := 0; column < width; column++ {
		start := column * frameCount / width
		end := (column + 1) * frameCount / width
		if end <= start {
			end = start + 1
		}
		if end > frameCount {
			end = frameCount
		}
		fillColumn(column, m.frames[start:end])
	}

	return grid
}

func (m *audioMeter) NamedBands() []namedBandLevel {
	levels := []namedBandLevel{
		{Label: "Bass"},
		{Label: "Mid"},
		{Label: "Treb"},
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.bins) == 0 {
		return levels
	}

	maxFreq := math.Min(spectrumMaxFreq, m.sampleRate/2)
	if maxFreq <= spectrumMinFreq {
		maxFreq = spectrumMaxFreq
	}

	for i := range levels {
		var low, high float64
		switch levels[i].Label {
		case "Bass":
			low, high = 32, 250
		case "Mid":
			low, high = 250, 4000
		default:
			low, high = 4000, maxFreq
		}

		var peak float64
		for band := range m.bins {
			center := logarithmicFrequency((float64(band)+0.5)/float64(len(m.bins)), spectrumMinFreq, maxFreq)
			if center < low || center >= high {
				continue
			}
			if m.bins[band] > peak {
				peak = m.bins[band]
			}
		}
		levels[i].Value = peak
	}

	return levels
}

func analyzeSpectrum(window []float64, sampleRate float64, bandCount int) []float64 {
	if len(window) != spectrumFFTSize || bandCount <= 0 {
		return make([]float64, max(0, bandCount))
	}
	if sampleRate <= 0 {
		sampleRate = 48000
	}

	input := make([]complex128, spectrumFFTSize)
	for i, sample := range window {
		windowGain := 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(spectrumFFTSize-1)))
		input[i] = complex(sample*windowGain, 0)
	}

	fft(input)

	nyquist := sampleRate / 2
	maxFreq := math.Min(spectrumMaxFreq, nyquist)
	if maxFreq <= spectrumMinFreq {
		maxFreq = nyquist
	}

	bands := make([]float64, bandCount)
	for i := 0; i < bandCount; i++ {
		low := logarithmicFrequency(float64(i)/float64(bandCount), spectrumMinFreq, maxFreq)
		high := logarithmicFrequency(float64(i+1)/float64(bandCount), spectrumMinFreq, maxFreq)

		start := frequencyBin(low, sampleRate)
		end := frequencyBin(high, sampleRate)
		if start < 1 {
			start = 1
		}
		if end <= start {
			end = start + 1
		}
		if end > spectrumFFTSize/2 {
			end = spectrumFFTSize / 2
		}

		var power float64
		count := 0
		for k := start; k < end; k++ {
			magnitude := cmplx.Abs(input[k]) * 2 / float64(spectrumFFTSize)
			power += magnitude * magnitude
			count++
		}
		if count == 0 {
			continue
		}

		rms := math.Sqrt(power / float64(count))
		db := 20 * math.Log10(rms+1e-9)
		normalized := (db - spectrumFloorDB) / -spectrumFloorDB
		normalized = max(0, min(normalized, 1))

		// Slightly lift quiet details so the spectrum stays legible in a text UI.
		bands[i] = math.Pow(normalized, 0.8)
	}

	return bands
}

func logarithmicFrequency(position, minFreq, maxFreq float64) float64 {
	if maxFreq <= minFreq {
		return minFreq
	}
	return minFreq * math.Pow(maxFreq/minFreq, position)
}

func frequencyBin(freq, sampleRate float64) int {
	if sampleRate <= 0 || freq <= 0 {
		return 0
	}

	bin := int(math.Round(freq * float64(spectrumFFTSize) / sampleRate))
	return max(0, min(bin, spectrumFFTSize/2))
}

func fft(values []complex128) {
	n := len(values)
	if n <= 1 {
		return
	}

	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j &^= bit
		}
		j |= bit
		if i < j {
			values[i], values[j] = values[j], values[i]
		}
	}

	for length := 2; length <= n; length <<= 1 {
		angle := -2 * math.Pi / float64(length)
		step := cmplx.Exp(complex(0, angle))
		for offset := 0; offset < n; offset += length {
			w := complex(1, 0)
			half := length / 2
			for i := 0; i < half; i++ {
				even := values[offset+i]
				odd := values[offset+i+half] * w
				values[offset+i] = even + odd
				values[offset+i+half] = even - odd
				w *= step
			}
		}
	}
}

type meteredStreamer struct {
	streamer beep.Streamer
	meter    *audioMeter
}

func (s meteredStreamer) Stream(samples [][2]float64) (int, bool) {
	n, ok := s.streamer.Stream(samples)
	if n > 0 && s.meter != nil {
		s.meter.Process(samples[:n])
	}
	return n, ok
}

func (s meteredStreamer) Err() error {
	return s.streamer.Err()
}

func (m *model) visualizerStreamer(streamer beep.Streamer) beep.Streamer {
	if m.meter == nil {
		return streamer
	}
	return meteredStreamer{
		streamer: streamer,
		meter:    m.meter,
	}
}

func (m model) volumeScale() float64 {
	if m.volume <= 0 {
		return 0
	}
	return float64(m.volume) / 100
}

func (m model) volumeLabel() string {
	if m.muted || m.volume <= 0 {
		return "muted"
	}
	return fmt.Sprintf("%d%%", m.volume)
}

func (m *model) playbackStreamer(streamer beep.Streamer) beep.Streamer {
	streamer = m.visualizerStreamer(streamer)

	scale := m.volumeScale()
	volume := &effects.Volume{
		Streamer: streamer,
		Base:     2,
		Silent:   m.muted || scale <= 0,
	}
	if scale > 0 {
		volume.Volume = math.Log2(scale)
	}
	return volume
}

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
	m.playing.Control.Streamer = m.playbackStreamer(streamer)
	speaker.Unlock()

	return nil
}

func (m *model) adjustVolume(delta int) error {
	m.volume = max(0, min(m.volume+delta, maxVolumePercent))
	if !m.isPlaying() {
		return nil
	}
	return m.updatePlaybackLoop()
}

func (m *model) toggleMute() error {
	m.muted = !m.muted
	if !m.isPlaying() {
		return nil
	}
	return m.updatePlaybackLoop()
}

func (m *model) finishCurrentTrack() (tea.Cmd, error) {
	if m.transitioning {
		return nil, nil
	}
	previous := m.playing.Control.Source
	if m.loopMode == loopQueue {
		m.enqueueTrack(m.playingPath, m.playing.Title)
	}
	if next, ok := m.dequeueNext(); ok {
		m.transitioning = true
		return m.playSongCmdWithPrevious(next.path, previous), nil
	}
	return nil, m.stopPlayback()
}

func (m *model) seekBy(delta time.Duration) (tea.Cmd, error) {
	if !m.isPlaying() || m.playing.Format == nil || m.playing.Control.Source == nil {
		return nil, nil
	}

	sampleDelta := m.playing.Format.SampleRate.N(delta)
	current := m.playing.Control.Source.Position()
	target := current + sampleDelta
	if target < 0 {
		target = 0
	}
	if length := m.playing.Control.Source.Len(); target >= length {
		return m.finishCurrentTrack()
	}

	speaker.Lock()
	defer speaker.Unlock()
	return nil, m.playing.Control.Source.Seek(target)
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

func (m *model) playSongCmdWithPrevious(path string, previous beep.StreamSeekCloser) tea.Cmd {
	return func() tea.Msg {
		f, err := os.Open(path)
		if err != nil {
			return errorMsg(fmt.Errorf("open %s: %w", filepath.Base(path), err))
		}

		var (
			streamer beep.StreamSeekCloser
			format   beep.Format
		)
		switch strings.ToLower(filepath.Ext(path)) {
		case ".mp3":
			streamer, format, err = mp3.Decode(f)
		case ".wav":
			streamer, format, err = wav.Decode(f)
		default:
			err = fmt.Errorf("unsupported audio format: %s", filepath.Ext(path))
		}
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

func (m *model) playSongCmd(path string) tea.Cmd {
	return m.playSongCmdWithPrevious(path, m.playing.Control.Source)
}

func closeStream(stream beep.StreamSeekCloser) error {
	if stream == nil {
		return nil
	}
	err := stream.Close()
	if err != nil && errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

func (m *model) stopPlayback() error {
	if m.playing.Control.Source == nil {
		m.transitioning = false
		return nil
	}

	speaker.Clear()
	err := closeStream(m.playing.Control.Source)
	m.playing = track.Track{}
	m.playingPath = ""
	m.transitioning = false
	if m.meter != nil {
		m.meter.Reset()
	}
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

func spectrogramPanelStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(boundedWidth(width)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#f9e2af")).
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
	return lipgloss.NewStyle().Width(boundedWidth(width)).Render(s)
}

func wrapQueueItem(prefix, title string, width int) string {
	if width <= 0 {
		return ""
	}

	prefixWidth := ansi.StringWidth(prefix)
	if prefixWidth >= width {
		return ansi.Truncate(prefix+title, width, "")
	}

	bodyWidth := width - prefixWidth
	wrapped := ansi.Wrap(title, bodyWidth, " ")
	lines := strings.Split(wrapped, "\n")
	for i, line := range lines {
		if i == 0 {
			lines[i] = prefix + line
			continue
		}
		lines[i] = strings.Repeat(" ", prefixWidth) + line
	}
	return strings.Join(lines, "\n")
}

func appendQueueBlock(lines *[]string, block string) (start, end int) {
	start = len(*lines)
	*lines = append(*lines, strings.Split(block, "\n")...)
	end = len(*lines)
	return start, end
}

func queueViewport(lines []string, contentHeight, selectedStart, selectedEnd int) []string {
	if contentHeight < 0 || len(lines) <= contentHeight {
		return lines
	}

	if selectedStart < 0 {
		selectedStart = 0
		selectedEnd = 0
	}
	if selectedEnd < selectedStart {
		selectedEnd = selectedStart
	}

	start := 0
	if selectedEnd > contentHeight {
		start = selectedEnd - contentHeight
	}
	if selectedStart < start {
		start = selectedStart
	}

	maxStart := len(lines) - contentHeight
	if start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}

	end := start + contentHeight
	if end > len(lines) {
		end = len(lines)
	}

	return lines[start:end]
}

func (m *model) queueViewWithWidth(width int) string {
	return m.queueViewWithSize(width, -1)
}

func (m *model) queueViewWithSize(width, contentHeight int) string {
	queueStyle := queuePanelStyle(m.focus == focusQueue, width)
	contentWidth := boundedWidth(width - queueStyle.GetHorizontalFrameSize())

	lines := []string{fmt.Sprintf("Queue (%d)", len(m.queue))}
	selectedStart, selectedEnd := -1, -1
	if m.playing.Title != "" {
		lines = append(lines, "", "Playing")
		appendQueueBlock(&lines, wrapQueueItem("  ", m.playing.Title, contentWidth))
	}

	lines = append(lines, "", "Up Next")
	if len(m.queue) == 0 {
		lines = append(lines, "  (empty)")
	} else {
		for i, item := range m.queue {
			prefix := "  "
			if m.focus == focusQueue && i == m.queueCursor {
				prefix = "› "
			}
			start, end := appendQueueBlock(&lines, wrapQueueItem(fmt.Sprintf("%s%d. ", prefix, i+1), item.title, contentWidth))
			if m.focus == focusQueue && i == m.queueCursor {
				selectedStart, selectedEnd = start, end
			}
		}
	}

	if contentHeight >= 0 {
		lines = queueViewport(lines, contentHeight, selectedStart, selectedEnd)
	}

	content := lipgloss.NewStyle().
		Width(contentWidth).
		Height(max(contentHeight, len(lines))).
		Render(strings.Join(lines, "\n"))

	return queueStyle.Render(content)
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

func compactVisualizerWidth(width int) int {
	if width <= 0 {
		return 0
	}

	target := int(math.Round(float64(width) * visualizerWidthRatio))
	target = max(minVisualizerWidth, min(target, maxVisualizerWidth))
	if target > width {
		target = width
	}
	return target
}

func compactSpectrogramHeight(windowHeight int) int {
	target := windowHeight / 4
	target = max(minSpectrogramHeight, min(target, maxSpectrogramHeight))
	return target
}

func spectrogramCell(normalized float64) string {
	levels := []rune(" .:-=+*#%@")
	level := int(math.Round(normalized * float64(len(levels)-1)))
	level = max(0, min(level, len(levels)-1))

	return lipgloss.NewStyle().Foreground(lipgloss.Color(spectrogramColor(normalized))).Render(string(levels[level]))
}

func spectrogramColor(normalized float64) string {
	color := "#6c7086"
	switch {
	case normalized > 0.82:
		color = "#f38ba8"
	case normalized > 0.68:
		color = "#fab387"
	case normalized > 0.52:
		color = "#f9e2af"
	case normalized > 0.36:
		color = "#a6e3a1"
	case normalized > 0.20:
		color = "#74c7ec"
	}
	return color
}

func bandMeter(value float64, width int) string {
	if width <= 0 {
		return ""
	}

	filled := int(math.Round(float64(width) * value))
	filled = max(0, min(filled, width))

	var b strings.Builder
	b.Grow(width)
	for i := 0; i < width; i++ {
		ch := "·"
		color := "#585b70"
		if i < filled {
			ch = "█"
			color = spectrogramColor(value)
		}
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(ch))
	}
	return b.String()
}

func spectrogramRowLabel(row, height int) string {
	switch {
	case row == 0:
		return "Treb"
	case row == height/2:
		return "Mid "
	case row == height-1:
		return "Bass"
	default:
		return "    "
	}
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

func (m model) queueStatus() string {
	if len(m.queue) == 0 {
		return "queue empty"
	}
	return fmt.Sprintf("%d queued", len(m.queue))
}

func (m model) playbackMeta() string {
	parts := []string{
		fmt.Sprintf("vol %s", m.volumeLabel()),
		m.queueStatus(),
	}
	if m.loopMode != loopOff {
		parts = append(parts, fmt.Sprintf("loop %s", m.loopMode))
	}
	return strings.Join(parts, " • ")
}

func (m model) playerHelpView() string {
	contentWidth := m.playerHelpContentWidth()
	statusStyle := lipgloss.NewStyle().Padding(0, 1).MaxWidth(contentWidth)

	lines := make([]string, 0, 4)
	if m.err != nil {
		lines = append(lines, statusStyle.Render(fmt.Sprintf("❌ Error: %v", m.err)))
	} else if m.playing.Title != "" {
		statusText := fmt.Sprintf("▶ Now Playing: %s", m.playing.Title)
		if m.playing.Control.Paused {
			statusText = fmt.Sprintf("⏸ Paused: %s", m.playing.Title)
		}
		lines = append(lines, statusStyle.Render(statusText))
		lines = append(lines, statusStyle.Render(m.playing.String()))
		lines = append(lines, statusStyle.Render(m.playbackMeta()))
	} else {
		statusText := "Select an audio file to play."
		lines = append(lines, statusStyle.Render(statusText))
		lines = append(lines, statusStyle.Render(m.playbackMeta()))
	}

	if helpView := m.help.ViewWithWidth(m.helpFocus(), contentWidth); helpView != "" {
		lines = append(lines, helpView)
	}

	return playerHelpPanelStyle().
		Width(m.playerHelpPanelWidth()).
		Render(truncateBlock(strings.Join(lines, "\n"), contentWidth))
}

func (m model) visualizerView(width, plotHeight int) string {
	if width <= 0 || plotHeight <= 0 {
		return ""
	}

	const labelWidth = 4
	plotAreaWidth := boundedWidth(width - labelWidth - 1)
	plotWidth := compactVisualizerWidth(plotAreaWidth)
	if plotWidth < 12 {
		plotWidth = plotAreaWidth
	}
	if plotWidth < 1 {
		plotWidth = 1
	}

	grid := make([][]float64, plotHeight)
	if m.meter != nil {
		grid = m.meter.Spectrogram(plotWidth, plotHeight)
	} else {
		for i := range grid {
			grid[i] = make([]float64, plotWidth)
		}
	}

	lines := make([]string, 0, plotHeight+2)
	lines = append(lines, "Sound Map")
	if m.meter != nil {
		levels := m.meter.NamedBands()
		parts := make([]string, 0, len(levels))
		for _, level := range levels {
			parts = append(parts, fmt.Sprintf("%s %s", level.Label, bandMeter(level.Value, 4)))
		}
		lines = append(lines, ansi.Truncate(strings.Join(parts, " "), width, ""))
	} else {
		lines = append(lines, "Bass ···· Mid ···· Treb ····")
	}
	for i, row := range grid {
		var b strings.Builder
		for _, intensity := range row {
			b.WriteString(spectrogramCell(intensity))
		}
		line := spectrogramRowLabel(i, plotHeight) + " " + lipgloss.NewStyle().Width(plotAreaWidth).Align(lipgloss.Center).Render(b.String())
		lines = append(lines, ansi.Truncate(line, width, ""))
	}
	lines = append(lines, ansi.Truncate(strings.Repeat(" ", labelWidth+1)+"older → newer", width, ""))

	return strings.Join(lines, "\n")
}

func (m model) spectrogramPanelView(width, plotHeight int) string {
	style := spectrogramPanelStyle(width)
	contentWidth := boundedWidth(width - style.GetHorizontalFrameSize())
	return style.Render(truncateBlock(m.visualizerView(contentWidth, plotHeight), contentWidth))
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

func (m *model) syncTracksViewportHeight() {
	bottom := m.playerHelpView()
	topHeight := m.topPaneHeight(bottom)
	m.tracks.setHeight(m.tracksViewHeight(topHeight))
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
	queueFrameHeight := queuePanelStyle(m.focus == focusQueue, sizing.queueWidth).GetVerticalFrameSize()
	spectrogramFrameHeight := spectrogramPanelStyle(sizing.queueWidth).GetVerticalFrameSize()
	maxSpectrogramHeight := topHeight - rightPaneGap - minQueueContentHeight - queueFrameHeight - spectrogramFrameHeight - 2
	if maxSpectrogramHeight < 1 {
		maxSpectrogramHeight = 1
	}
	spectrogramHeight := min(compactSpectrogramHeight(topHeight), maxSpectrogramHeight)
	spectrogram := m.spectrogramPanelView(sizing.queueWidth, spectrogramHeight)
	queueContentHeight := topHeight - lipgloss.Height(spectrogram) - rightPaneGap - queueFrameHeight
	if queueContentHeight < 0 {
		queueContentHeight = 0
	}
	queue := m.queueViewWithSize(sizing.queueWidth, queueContentHeight)
	right := queue + strings.Repeat("\n", rightPaneGap) + spectrogram
	trackStyle := trackPanelStyle(m.focus == focusTracks)
	leftContentWidth := boundedWidth(sizing.leftWidth - trackStyle.GetHorizontalFrameSize())
	var leftPane strings.Builder
	leftPane.WriteString(m.tracks.ViewWithHeight(m.tracksViewHeight(topHeight)))
	left := trackStyle.
		Width(sizing.leftWidth).
		Render(truncateBlock(leftPane.String(), leftContentWidth))

	top := lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", sizing.gap), right)
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

		case key.Matches(msg, m.help.Keys().Global.SeekBack):
			cmd, err := m.seekBy(-seekStep)
			if err != nil {
				m.err = err
			} else {
				m.err = nil
			}
			return m, cmd

		case key.Matches(msg, m.help.Keys().Global.SeekAhead):
			cmd, err := m.seekBy(seekStep)
			if err != nil {
				m.err = err
			} else {
				m.err = nil
			}
			return m, cmd

		case key.Matches(msg, m.help.Keys().Global.VolumeDown):
			if err := m.adjustVolume(-volumeStep); err != nil {
				m.err = err
			} else {
				m.err = nil
			}
			return m, nil

		case key.Matches(msg, m.help.Keys().Global.VolumeUp):
			if err := m.adjustVolume(volumeStep); err != nil {
				m.err = err
			} else {
				m.err = nil
			}
			return m, nil

		case key.Matches(msg, m.help.Keys().Global.Mute):
			if err := m.toggleMute(); err != nil {
				m.err = err
			} else {
				m.err = nil
			}
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
		m.transitioning = false
		m.err = msg
		return m, nil

	case dirLoadedMsg:
		m.tracks.loadingDirectory = false
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case loadedTrackMsg:
		m.transitioning = false
		speaker.Clear()
		if msg.previous != nil {
			if err := closeStream(msg.previous); err != nil {
				log.Printf("error closing previous track: %v", err)
			}
		}

		m.playing = msg.track
		m.playingPath = msg.path
		m.playing.Control.Paused = false
		m.err = nil
		if m.meter != nil {
			m.meter.Reset()
			m.meter.SetSampleRate(m.playing.Format.SampleRate)
		}

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
			cmd, err := m.finishCurrentTrack()
			if err != nil {
				m.err = err
				return m, nil
			}
			m.err = nil
			return m, cmd
		}
		return m, tickCmd()

	}

	m.syncTracksViewportHeight()
	cmd, path, didSelect := m.tracks.Update(msg)
	if didSelect {
		if isSupportedAudioFile(path) {
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
	fp.AllowedTypes = supportedAudioExtensions()
	fp.CurrentDirectory = initPath

	var sr beep.SampleRate = 48000

	m := model{
		tracks:     newTracksComponent(fp),
		sampleRate: sr,
		help:       help.NewDefault(),
		volume:     100,
		meter:      newAudioMeter(96),
	}

	speaker.Init(m.sampleRate, m.sampleRate.N(time.Second/10))

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}
