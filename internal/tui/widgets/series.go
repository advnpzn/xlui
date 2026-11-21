// Package widgets contains presentation behavior shared by terminal screens.
package widgets

import (
	"math"
	"strconv"
	"strings"
)

// Window is the number of recently observed sampling intervals retained by a Series.
const Window = 60

// Series is a fixed-size history of nonnegative measurements. Its zero value is ready to use.
type Series struct {
	values [Window]float64
	next   int
	count  int
}

func (s *Series) Push(value float64, available bool) {
	if !available || value < 0 || math.IsInf(value, 0) {
		value = math.NaN()
	} else if value < math.MaxFloat64/10 {
		// Match the printed scale's precision so sub-tenth jitter stays flat.
		value = math.Round(value*10) / 10
	}
	s.values[s.next] = value
	s.next = (s.next + 1) % Window
	if s.count < Window {
		s.count++
	}
}

// Skip records omitted observations. Even a long outage takes at most Window writes.
func (s *Series) Skip(n uint64) {
	for i := uint64(0); i < min(n, uint64(Window)); i++ {
		s.Push(0, false)
	}
}

// Plot returns one ASCII character per visible sample, newest on the right, and
// the observed scale in the given unit. A question mark is an unavailable value.
func (s *Series) Plot(width int, unit string) (string, string) {
	n := min(width, s.count)
	if n <= 0 {
		return "", "unavailable"
	}
	start := (s.next - n + Window) % Window
	low, high := math.Inf(1), math.Inf(-1)
	for i := 0; i < n; i++ {
		v := s.values[(start+i)%Window]
		if !math.IsNaN(v) {
			low = min(low, v)
			high = max(high, v)
		}
	}

	const levels = ".:-=+*#%@"
	var line strings.Builder
	line.Grow(n)
	for i := 0; i < n; i++ {
		v := s.values[(start+i)%Window]
		switch {
		case math.IsNaN(v):
			line.WriteByte('?')
		case high == low:
			line.WriteByte('-')
		default:
			level := math.Round((v - low) / (high - low) * float64(len(levels)-1))
			line.WriteByte(levels[int(level)])
		}
	}

	if math.IsInf(low, 1) {
		return line.String(), "unavailable"
	}
	scale := format(low)
	if high != low {
		scale += ".." + format(high)
	}
	return line.String(), scale + " " + unit
}

func format(v float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(v, 'f', 1, 64), ".0")
}

// Bars draws a compact sparkline on a zero-to-peak scale. Missing samples
// render as dots. Plot provides the explicit range for the expanded inspector.
func (s *Series) Bars(width int) string {
	n := min(width, s.count)
	if n <= 0 {
		return ""
	}
	start := (s.next - n + Window) % Window
	peak := 0.0
	for i := range n {
		v := s.values[(start+i)%Window]
		if !math.IsNaN(v) {
			peak = max(peak, v)
		}
	}
	levels := [...]rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	var line strings.Builder
	for i := range n {
		v := s.values[(start+i)%Window]
		if math.IsNaN(v) {
			line.WriteRune('·')
			continue
		}
		level := 0
		if peak > 0 {
			level = int(math.Round((v / peak) * 7))
		}
		line.WriteRune(levels[level])
	}
	return line.String()
}
