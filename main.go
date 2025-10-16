package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/filepicker"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
)

type directoryReadMsg struct {
	path    string
	entries []os.DirEntry
}

type model struct {
	playing    string
	filepicker filepicker.Model
	err        error
}

type (
	songPlayingMsg string
	errorMsg       error
)

func playSongCmd(path string) tea.Cmd {
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

		speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))

		speaker.Play(streamer)

		return nil
	}
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
	} else if m.playing != "" {
		builder.WriteString(statusStyle.Render(fmt.Sprintf("🎵 Now Playing: %s", m.playing)))
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
		m.playing = string(msg)
		m.err = nil
		return m, nil

	case errorMsg:
		m.err = msg
		return m, nil
	}
	var cmd tea.Cmd
	m.filepicker, cmd = m.filepicker.Update(msg)

	if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
		if strings.HasSuffix(strings.ToLower(path), ".mp3") {
			m.playing = filepath.Base(path)
			return m, playSongCmd(path)
		}
	}
	m.playing = ""
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
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}
