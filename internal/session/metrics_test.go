package session

import (
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/xen"
)

func checkCPU(t *testing.T, snapshot Snapshot, d xen.Domain, want float64, valid bool) {
	t.Helper()
	got, ok := snapshot.CPUPercent(d)
	if got != want || ok != valid {
		t.Fatalf("domain %d rate at frame %d: got (%g, %t), want (%g, %t)", d.ID, snapshot.Sequence, got, ok, want, valid)
	}
}

func TestCPUPercentUsesElapsedTime(t *testing.T) {
	start := time.Now()
	d := xen.Domain{ID: 8, UUID: "worker", CPUSeconds: 12}
	first := Snapshot{}.afterRead([]xen.Domain{d}, start, nil)
	checkCPU(t, first, d, 0, false)

	d.Name = "renamed"
	d.CPUSeconds = 15.75
	active := first.afterRead([]xen.Domain{d}, start.Add(2500*time.Millisecond), nil)
	checkCPU(t, active, d, 150, true)
	idle := active.afterRead([]xen.Domain{d}, start.Add(4*time.Second), nil)
	checkCPU(t, idle, d, 0, true)

	d.CPUSeconds = 0
	reset := idle.afterRead([]xen.Domain{d}, start.Add(5*time.Second), nil)
	checkCPU(t, reset, d, 0, false)
	d.CPUSeconds = 0.25
	recovered := reset.afterRead([]xen.Domain{d}, start.Add(6*time.Second), nil)
	checkCPU(t, recovered, d, 25, true)
	checkCPU(t, active, d, 150, true) // Older frames remain immutable.
}

func TestCPUPercentFailureBreaksBaseline(t *testing.T) {
	start := time.Now()
	d := xen.Domain{ID: 1, UUID: "first", CPUSeconds: 0}
	first := Snapshot{}.afterRead([]xen.Domain{d}, start, nil)
	d.CPUSeconds++
	active := first.afterRead([]xen.Domain{d}, start.Add(time.Second), nil)
	outage := errors.New("offline")
	stale := active.afterRead(nil, start.Add(2*time.Second), outage)
	if stale.Err != outage || stale.ObservedAt != active.ObservedAt || len(stale.Domains) != 1 || stale.Sequence != 3 {
		t.Fatalf("stale frame: %+v", stale)
	}
	checkCPU(t, stale, d, 0, false)
	checkCPU(t, active, d, 100, true)
	d.CPUSeconds++
	reconnect := stale.afterRead([]xen.Domain{d}, start.Add(3*time.Second), nil)
	checkCPU(t, reconnect, d, 0, false)
	if reconnect.Err != nil || reconnect.Sequence != 4 {
		t.Fatalf("recovered frame: %+v", reconnect)
	}
	d.CPUSeconds += 3
	next := reconnect.afterRead([]xen.Domain{d}, start.Add(5*time.Second), nil)
	checkCPU(t, next, d, 150, true)
}

func TestCPUPercentIsBoundToVisibleInstances(t *testing.T) {
	start := time.Now()
	d := xen.Domain{ID: 1, UUID: "first", CPUSeconds: 5}
	initial := Snapshot{}.afterRead([]xen.Domain{d}, start, nil)
	for _, replacement := range []xen.Domain{
		{ID: 1, UUID: "different", CPUSeconds: 8},
		{ID: 2, UUID: "first", CPUSeconds: 8},
	} {
		next := initial.afterRead([]xen.Domain{replacement}, start.Add(time.Second), nil)
		checkCPU(t, next, replacement, 0, false)
		checkCPU(t, next, d, 0, false)
	}
	empty := initial.afterRead(nil, start.Add(time.Second), nil)
	if len(empty.cpu) != 0 {
		t.Fatalf("retained %d removed instances", len(empty.cpu))
	}
	back := empty.afterRead([]xen.Domain{d}, start.Add(2*time.Second), nil)
	checkCPU(t, back, d, 0, false)
}

func TestCPUPercentDiscardsInvalidSamples(t *testing.T) {
	start := time.Now()
	d := xen.Domain{UUID: "worker", CPUSeconds: 1}
	first := Snapshot{}.afterRead([]xen.Domain{d}, start, nil)
	for _, invalid := range []float64{-1, math.NaN(), math.Inf(1)} {
		bad := d
		bad.CPUSeconds = invalid
		s := first.afterRead([]xen.Domain{bad}, start.Add(time.Second), nil)
		checkCPU(t, s, bad, 0, false)
		checkCPU(t, s.afterRead([]xen.Domain{d}, start.Add(2*time.Second), nil), d, 0, false)
	}
	for _, elapsed := range []time.Duration{0, -time.Second} {
		checkCPU(t, first.afterRead([]xen.Domain{d}, start.Add(elapsed), nil), d, 0, false)
	}
}

func TestCPUIntervalOnClockDiscontinuity(t *testing.T) {
	for _, tc := range []struct {
		monotonic, wall time.Duration
		want            float64
	}{
		{2 * time.Second, 2*time.Second + time.Millisecond, 2},
		{time.Minute, time.Minute, 60},
		{2 * time.Second, 15 * time.Minute, 0},
		{3 * time.Second, time.Second, 0},
		{time.Second, -time.Second, 0},
	} {
		if got := cpuInterval(tc.monotonic, tc.wall); got != tc.want {
			t.Errorf("cpuInterval(%v, %v) = %v, want %v", tc.monotonic, tc.wall, got, tc.want)
		}
	}
}

// Time publishing CPU measurements separately from Xen collection and parsing.
func BenchmarkCPUAfterRead256(b *testing.B) {
	before := make([]xen.Domain, 256)
	after := make([]xen.Domain, len(before))
	for id := range before {
		before[id] = xen.Domain{ID: id, UUID: strconv.Itoa(id), CPUSeconds: 10}
		after[id] = before[id]
		after[id].CPUSeconds++
	}
	start := time.Now()
	base := Snapshot{}.afterRead(before, start, nil)
	end := start.Add(2 * time.Second)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame := base.afterRead(after, end, nil)
		if p, ok := frame.CPUPercent(after[0]); !ok || p != 50 {
			b.Fatalf("unexpected rate %g, %t", p, ok)
		}
	}
}

func TestProviderRateNeedsNoCounterAndDisappearsOnFailure(t *testing.T) {
	now := time.Now()
	value := 180.0
	d := xen.Domain{ID: 3, UUID: "guest", VCPUs: 2, CPUSeconds: -1, CPUPercent: &value}
	first := Snapshot{}.afterRead([]xen.Domain{d}, now, nil)
	checkCPU(t, first, d, 180, true)
	checkCPU(t, first.afterRead(nil, now.Add(time.Second), errors.New("offline")), d, 0, false)
	d.CPUPercent = nil
	checkCPU(t, first.afterRead([]xen.Domain{d}, now.Add(time.Second), nil), d, 0, false)
}
