package track

import (
	"strings"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
)

type testStream struct {
	len      int
	position int
}

func (s *testStream) Stream(samples [][2]float64) (int, bool) {
	return 0, false
}

func (s *testStream) Err() error {
	return nil
}

func (s *testStream) Len() int {
	return s.len
}

func (s *testStream) Position() int {
	return s.position
}

func (s *testStream) Seek(p int) error {
	s.position = p
	return nil
}

func (s *testStream) Close() error {
	return nil
}

func TestStringUsesSingleMillisecondSnapshotForTimeDisplay(t *testing.T) {
	source := &testStream{len: 20, position: 4}
	format := beep.Format{SampleRate: 10, NumChannels: 2, Precision: 2}
	track := New(source, &format, "short.mp3", 2*time.Second)

	got := track.String()
	if !strings.Contains(got, "0:00.400 / 0:02.000 (-0:01.600)") {
		t.Fatalf("track string = %q, want consistent millisecond display", got)
	}
}
