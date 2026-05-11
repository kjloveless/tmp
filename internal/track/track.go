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

func (t Track) Duration() time.Duration {
	return t.length
}

func (t Track) Remaining() time.Duration {
	remaining := t.length - t.Position()
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (t Track) Percent() float64 {
	if t.length <= 0 {
		return 0
	}
	return t.Position().Seconds() / t.length.Seconds()
}

func displayDuration(d time.Duration) string {
	if d <= 0 {
		return "0:00.000"
	}
	d = d.Truncate(time.Millisecond)

	totalMilliseconds := d / time.Millisecond
	hours := totalMilliseconds / (60 * 60 * 1000)
	minutes := (totalMilliseconds / (60 * 1000)) % 60
	seconds := (totalMilliseconds / 1000) % 60
	milliseconds := totalMilliseconds % 1000

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d.%03d", hours, minutes, seconds, milliseconds)
	}
	return fmt.Sprintf("%d:%02d.%03d", minutes, seconds, milliseconds)
}

func (t Track) String() string {
	position := t.Position()
	if position < 0 {
		position = 0
	}
	if position > t.length {
		position = t.length
	}

	remaining := t.length - position
	if remaining < 0 {
		remaining = 0
	}
	percent := 0.0
	if t.length > 0 {
		percent = position.Seconds() / t.length.Seconds()
	}

	return fmt.Sprintf(
		"%s %s / %s (-%s)",
		t.progress.ViewAs(percent),
		displayDuration(position),
		displayDuration(t.length),
		displayDuration(remaining))
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
