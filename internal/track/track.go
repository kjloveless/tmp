package track

import (
	"fmt"
	"time"

	"github.com/kjloveless/tmp/internal/control"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/speaker"

	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
)

type Track struct {
	Control  control.Control
	Format   *beep.Format
	Title    string
	length   time.Duration
	progress progress.Model
}

func (t Track) Position() time.Duration {
	speaker.Lock()
	if t.Control.Source != nil {
		duration := t.Format.SampleRate.D(t.Control.Source.Position())
		speaker.Unlock()
		return duration
	}
	speaker.Unlock()
	panic("failure to retrieve position from track")
}

func (t Track) Percent() float64 {
	return t.Position().Seconds() / t.length.Seconds()
}

func (t Track) String() string {
	return fmt.Sprintf(
		"%s %s : %s",
		t.progress.ViewAs(t.Percent()),
		t.Position().Round(time.Second),
		t.length.Round(time.Second))
}

func New(
	streamer beep.StreamSeekCloser,
	format *beep.Format,
	title string,
	length time.Duration,
) Track {
	prog := progress.New(
		progress.WithColors(lipgloss.Color("#ff7ccb"), lipgloss.Color("#fdff8c")),
		progress.WithScaled(true),
		progress.WithSpringOptions(6.0, .5),
	)
	prog.ShowPercentage = false

	control := control.New(streamer)
	return Track{
		Control:  control,
		Format:   format,
		Title:    title,
		length:   length,
		progress: prog,
	}
}
