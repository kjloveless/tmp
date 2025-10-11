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

	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
)

type model struct {
	songs  []string
	cursor int
	vp     viewport.Model
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
	return nil
}

func (m model) View() string {
	var builder strings.Builder
	for i, song := range m.songs {
		if m.cursor == i {
			fmt.Fprintf(&builder, "> %d: %s\n", i+1, song)
		} else {
			fmt.Fprintf(&builder, "%d: %s\n", i+1, song)
		}
	}
	builder.WriteString("---------END OF LIST---------")
	m.vp.SetContent(builder.String())

	return m.vp.View()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			} else {
				m.cursor = len(m.songs) - 1 // wrap around logic
			}
		case tea.KeyDown:
			if m.cursor < len(m.songs)-1 {
				m.cursor++
			} else {
				m.cursor = 0
			}

		case tea.KeyEnter:
			selectedSong := m.songs[m.cursor]
			return m, playSongCmd(selectedSong)
		}
	}

	return m, nil
}

func main() {
	root := "./sounds"
	songs := make([]string, 0, 5)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("prevent panic by handling error: %v\n", err)
			return err
		}
		if !info.IsDir() {
			songs = append(songs, path)
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	vp := viewport.New(0, len(songs))
	m := model{
		songs:  songs,
		cursor: 0,
		vp:     vp,
	}

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}
