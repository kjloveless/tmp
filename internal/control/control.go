package control

import (
	"github.com/gopxl/beep/v2"
)

type Control struct {
	*beep.Ctrl
	Source beep.StreamSeekCloser
	Loop   bool
}

func New(source beep.StreamSeekCloser) Control {
	return Control{
		Ctrl:   &beep.Ctrl{Streamer: source, Paused: false},
		Source: source,
		Loop:   false,
	}
}
