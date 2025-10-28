package track

import (
  "fmt"
  "time"

  "github.com/gopxl/beep/v2"
  "github.com/gopxl/beep/v2/speaker"
)

type Track struct {
	Ctrl   *beep.Ctrl
	Format *beep.Format
	Title  string
  Length time.Duration
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
	return t.Position().Seconds() / t.Length.Seconds()
}

func (t Track) String() string {
	return fmt.Sprintf(
		"%s : %s",
		t.Position().Round(time.Second),
		t.Length.Round(time.Second))
}


