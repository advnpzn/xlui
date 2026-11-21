package session

import (
	"math"
	"time"

	"github.com/advnpzn/xlui/internal/xen"
)

type cpuSample struct {
	seconds float64
	vcpus   int
	percent float64
	hasRate bool
}

// CPUPercent reports measured utilization across the domain's vCPUs. One
// fully used logical CPU is 100. The first observation and gaps have no rate.
func (s Snapshot) CPUPercent(domain xen.Domain) (float64, bool) {
	sample := s.cpu[domain.Identity()]
	return sample.percent, sample.hasRate
}

// afterRead publishes a fresh copy of the metrics for this sample. Readers
// can retain an older frame while polling builds or drops the next baseline.
func (s Snapshot) afterRead(domains []xen.Domain, at time.Time, err error) Snapshot {
	s.Sequence++
	if err != nil {
		s.Err = err
		s.cpu = nil
		return s
	}

	next := Snapshot{
		Sequence:   s.Sequence,
		Domains:    domains,
		ObservedAt: at,
		cpu:        make(map[xen.Identity]cpuSample, len(domains)),
	}
	elapsed := cpuInterval(at.Sub(s.ObservedAt), at.Round(0).Sub(s.ObservedAt.Round(0)))
	for _, d := range domains {
		if d.UUID == "" {
			continue
		}
		key := d.Identity()
		// XenAPI supplies a rate directly. Counter-based providers need a baseline.
		if v := d.CPUPercent; v != nil && *v >= 0 && !math.IsInf(*v, 0) {
			next.cpu[key] = cpuSample{seconds: -1, percent: *v, hasRate: true, vcpus: d.VCPUs}
			continue
		}
		if d.CPUSeconds < 0 || math.IsNaN(d.CPUSeconds) || math.IsInf(d.CPUSeconds, 0) {
			continue
		}
		current := cpuSample{seconds: d.CPUSeconds, vcpus: d.VCPUs}
		prev, exists := s.cpu[key]
		if exists && prev.seconds >= 0 && prev.vcpus == d.VCPUs && elapsed > 0 && current.seconds >= prev.seconds {
			rate := (current.seconds - prev.seconds) / elapsed * 100
			if !math.IsInf(rate, 0) {
				if d.VCPUs > 0 {
					rate = min(rate, 100*float64(d.VCPUs))
				}
				current.percent = rate
				current.hasRate = true
			}
		}
		next.cpu[key] = current
	}
	return next
}

// A workstation's monotonic clock can pause during suspend while a remote
// guest keeps running. A wall-clock step can also make the two clocks diverge.
// Treat a difference over one second as a gap and resume on the next sample.
func cpuInterval(monotonic, wall time.Duration) float64 {
	if monotonic <= 0 || wall <= 0 || (monotonic-wall).Abs() > time.Second {
		return 0
	}
	return monotonic.Seconds()
}
