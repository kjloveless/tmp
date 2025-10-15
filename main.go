package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
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
	currentPath string
	entries     []os.DirEntry
	cursor      int
	vp          viewport.Model
}

func readDirectory(path string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		log.Println("ERROR: Reading directories")
		return nil, err
	}
	return entries, nil
}

func changeDirectoryCmd(path string) tea.Cmd {
	return func() tea.Msg {
		entries, err := readDirectory(path)
		if err != nil {
			log.Println("ERROR: Reading directories")
			return nil
		}
		return directoryReadMsg{path: path, entries: entries}
	}
}

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
	return changeDirectoryCmd(m.currentPath)
}

func (m model) View() string {
	var builder strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	builder.WriteString(headerStyle.Render("Current Path: " + m.currentPath))
	builder.WriteString("\n")
	if m.cursor == 0 {
		builder.WriteString("> ..\n")
	} else {
		builder.WriteString(" ..\n")
	}
	for i, entry := range m.entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}

		if m.cursor == i+1 {
			fmt.Fprintf(&builder, "> %s\n", name)
		} else {
			fmt.Fprintf(&builder, "  %s\n", name)
		}
	}

	m.vp.SetContent(builder.String())

	return m.vp.View()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case directoryReadMsg:
		m.currentPath = msg.path
		m.entries = msg.entries
		m.cursor = 0
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {

		case tea.KeyEsc, tea.KeyCtrlC:
			return m, tea.Quit

		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			} else {
				m.cursor = len(m.entries)
			}
		case tea.KeyDown:
			if m.cursor < len(m.entries) {
				m.cursor++
			} else {
				m.cursor = 0
			}

		case tea.KeyEnter:
			var newPath string
			if m.cursor == 0 {
				newPath = filepath.Dir(m.currentPath)
				return m, changeDirectoryCmd(newPath)
			}
			selectedEntry := m.entries[m.cursor-1]
			newPath = filepath.Join(m.currentPath, selectedEntry.Name())
			if selectedEntry.IsDir() {
				return m, changeDirectoryCmd(newPath)
			} else if strings.HasSuffix(strings.ToLower(selectedEntry.Name()), ".mp3") {
				return m, playSongCmd(newPath)
			}
		}
	}

	return m, nil
}

func main() {
	initPath, err := filepath.Abs("./sounds")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := os.Stat(initPath); os.IsNotExist(err) {
		log.Fatalf("Directory does not exist: %s", initPath)
	}
	vp := viewport.New(80, 20)

	m := model{
		currentPath: initPath,
		cursor:      0,
		vp:          vp,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}
