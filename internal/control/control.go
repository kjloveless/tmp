package control

import (
  "github.com/gopxl/beep/v2"
)

type Control struct {
  *beep.Ctrl
  Loop bool
}

func New(ctrl *beep.Ctrl) Control {
  return Control{
    Ctrl: ctrl,
    Loop: false,
  }
}
