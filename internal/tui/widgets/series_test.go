package widgets

import (
	"math"
	"strings"
	"testing"
)

func TestSeriesShowsScaleGapsAndNewestSamples(t *testing.T) {
	var s Series
	s.Push(0, true)
	s.Push(0, false)
	s.Push(100, true)
	s.Push(200, true)
	if line, scale := s.Plot(80, "%"); line != ".?+@" || scale != "0..200 %" {
		t.Fatalf("full graph %q with scale %q", line, scale)
	}
	if line, scale := s.Plot(2, "%"); line != ".@" || scale != "100..200 %" {
		t.Fatalf("narrow graph %q with scale %q", line, scale)
	}
	if line, scale := s.Plot(1, "%"); line != "-" || scale != "200 %" {
		t.Fatalf("constant graph %q with scale %q", line, scale)
	}
	if line, _ := s.Plot(0, "%"); line != "" {
		t.Fatalf("zero-width graph contains %q", line)
	}
}

func TestSeriesBoundsHistoryAndInvalidSamples(t *testing.T) {
	var s Series
	s.Push(42, true)
	s.Skip(1 << 32)
	s.Push(math.Inf(1), true)
	s.Push(math.NaN(), true)
	if line, scale := s.Plot(1000, "MiB"); line != strings.Repeat("?", Window) || scale != "unavailable" {
		t.Fatalf("long gap %q with scale %q", line, scale)
	}
	s.Push(0, true)
	if line, scale := s.Plot(2, "MiB"); line != "?-" || scale != "0 MiB" {
		t.Fatalf("valid zero after gap %q with scale %q", line, scale)
	}
}

func TestSubTenthJitterStaysFlatAtLabelPrecision(t *testing.T) {
	var s Series
	s.Push(49.999, true)
	s.Push(50.001, true)
	if line, scale := s.Plot(2, "%"); line != "--" || scale != "50 %" {
		t.Fatalf("jitter shown as %q with scale %q", line, scale)
	}
}

func TestDashboardBarsShowZeroPeakAndGaps(t *testing.T) {
	var s Series
	s.Push(0, true)
	s.Skip(1)
	s.Push(100, true)
	if got := s.Bars(3); got != "▁·█" {
		t.Fatalf("bars %q", got)
	}
	if got := s.Bars(1); got != "█" {
		t.Fatalf("latest visible value %q", got)
	}
}
