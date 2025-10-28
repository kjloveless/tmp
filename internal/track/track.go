package track

import (
  "fmt"
  "time"

  "github.com/gopxl/beep/v2"
  "github.com/gopxl/beep/v2/speaker"

  "github.com/charmbracelet/bubbles/progress"
)

type Track struct {
	Ctrl      *beep.Ctrl
	Format    *beep.Format
  Title     string
  length    time.Duration
	progress  progress.Model
}

func (t Track) Position() time.Duration {
  speaker.Lock()
	if streamer, ok := t.Ctrl.Streamer.(beep.StreamSeeker); ok {
    duration := t.Format.SampleRate.D(streamer.Position())
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
  ctrl *beep.Ctrl, 
  format *beep.Format, 
  title string, 
  length time.Duration,
) Track {
	prog := progress.New(progress.WithScaledGradient("#ff7ccb", "#fdff8c"), progress.WithSpringOptions(6.0, .5))
	prog.ShowPercentage = false

  return Track{
    Ctrl: ctrl,
    Format: format,
    Title: title,
    length: length,
    progress: prog,
  }
}
