package main


import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/filepicker"
  "github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

  "github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
)

type track struct {
  streamer  beep.StreamSeeker
  format    *beep.Format
  title     string
}
func (t track) Position() time.Duration {
  return t.format.SampleRate.D(t.streamer.Position())
}
func (t track) Length() time.Duration {
  return t.format.SampleRate.D(t.streamer.Len())
}
func (t track) Percent() float64 {
  return t.Position().Round(time.Second).Seconds() / t.Length().Round(time.Second).Seconds()
}

type directoryReadMsg struct {
	path    string
	entries []os.DirEntry
}

type model struct {
  playing     track
  progress    progress.Model
  filepicker  filepicker.Model
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
			log.Println("Error decoding file:", m.err)
			return nil
		}
		title := filepath.Base(path)
    track := track{
      streamer: streamer, 
      format: &format,
      title: title}

		speaker.Init(
      track.format.SampleRate, 
      track.format.SampleRate.N(time.Second/10))

		speaker.Play(track.streamer)

		return track
	}
}

func tickCmd() tea.Cmd {
  return tea.Tick(time.Millisecond, func(t time.Time) tea.Msg {
    return tickMsg(t)
  })
}

func (m model) Init() tea.Cmd {
	return m.filepicker.Init()
}

func (m model) View() string {
	var builder strings.Builder

	builder.WriteString(m.filepicker.View())
	builder.WriteString("\n")
	statusStyle := lipgloss.NewStyle().Padding(0, 1)
	if m.err != nil {
		builder.WriteString(statusStyle.Render(fmt.Sprintf("❌ Error: %v", m.err)))
	} else if m.playing.title != "" {
		builder.WriteString(statusStyle.Render(fmt.Sprintf("🎵 Now Playing: %s", m.playing.title)))
    builder.WriteString(statusStyle.Render("\n" + m.progress.ViewAs(m.playing.Percent())))
	} else {
		builder.WriteString(statusStyle.Render("Select an MP3 file to play."))
	}

	return builder.String()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			return m, tea.Quit
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
    return m, tickCmd()

  case tickMsg:
    if m.playing.Percent() > 1.0 {
      return m, tea.Quit
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
	m.playing.title = ""
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

  prog := progress.New(progress.WithScaledGradient("#ff7ccb", "#fdff8c"))

	m := model{
		filepicker: fp,
    progress: prog,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}
