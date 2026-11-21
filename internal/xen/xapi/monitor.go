package xapi

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/advnpzn/xlui/internal/xen"
)

type liveMetrics struct {
	Memory json.Number `json:"memory_actual"`
	Count  json.Number `json:"VCPUs_number"`
}

func (m *Monitor) Read(ctx context.Context) ([]xen.Domain, error) {
	records, err := m.records(ctx)
	if err != nil {
		if errors.Is(err, errDenied) {
			return nil, fmt.Errorf("%w. Open Sessions and reconnect to authenticate again", err)
		}
		if errors.Is(err, errInvalidSession) {
			m.session = ""
		}
		return nil, err
	}
	var metrics map[string]liveMetrics
	// Some accounts can inspect inventory but cannot read metrics. Keep the
	// inventory useful and represent missing measurements explicitly.
	metricCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	if err := m.rpc.call(metricCtx, "VM_metrics.get_all_records", []string{m.session}, &metrics, inventoryLimit); err != nil {
		metrics = nil
	}
	cancel()
	rrdCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	usage := m.readRRD(rrdCtx)
	cancel()
	out := make([]xen.Domain, 0, len(records))
	for _, r := range records {
		d := xen.Domain{Name: *r.Name, UUID: r.UUID, State: r.PowerState, ID: -1, VCPUs: -1, MemoryMiB: -1, CPUSeconds: -1}
		if r.PowerState == "Running" || r.PowerState == "Paused" {
			if id, err := strconv.Atoi(r.DomainID.String()); err == nil && id >= 0 {
				d.ID = id
			}
			live := metrics[r.Metrics]
			if b, err := live.Memory.Int64(); err == nil && b >= 0 {
				d.MemoryMiB = b >> 20
			}
			if c, err := strconv.Atoi(live.Count.String()); err == nil && c > 0 {
				d.VCPUs = c
			}
			if sample, ok := usage[strings.ToLower(r.UUID)]; ok {
				if sample.memory != nil {
					d.MemoryMiB = *sample.memory >> 20
				}
				if sample.memory != nil && sample.free != nil && *sample.free <= *sample.memory/1024 {
					kib := *sample.memory/1024 - *sample.free
					d.GuestUsedKiB = &kib
				}
				if d.VCPUs > 0 && !sample.missingCPU && len(sample.cpu) == d.VCPUs {
					var pct float64
					for _, v := range sample.cpu {
						pct += 100 * v
					}
					d.CPUPercent = &pct
				}
			}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].UUID < out[j].UUID
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

type rrdSample struct {
	cpu          map[int]float64
	missingCPU   bool
	memory, free *int64
}

func (m *Monitor) readRRD(ctx context.Context) map[string]rrdSample {
	u, _ := url.Parse(m.rpc.endpoint)
	now := time.Now()
	u.Path = "/rrd_updates"
	u.RawPath = ""
	u.RawQuery = url.Values{"start": {strconv.FormatInt(now.Add(-30*time.Second).Unix(), 10)}, "cf": {"AVERAGE"}, "host": {"false"}, "session_id": {m.session}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/xml")
	// The verified client refuses redirects. A URL containing the session is
	// never returned in errors or sent to another pool member.
	resp, err := m.rpc.http.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, inventoryLimit+1))
	if err != nil || len(data) > inventoryLimit {
		return nil
	}
	return parseRRD(data, now)
}

func parseRRD(data []byte, now time.Time) map[string]rrdSample {
	type row struct {
		Time   int64    `xml:"t"`
		Values []string `xml:"v"`
	}
	var doc struct {
		Legend []string `xml:"meta>legend>entry"`
		Rows   []row    `xml:"data>row"`
	}
	if xml.Unmarshal(data, &doc) != nil {
		return nil
	}
	var last row
	for _, r := range doc.Rows {
		if r.Time > last.Time && r.Time <= now.Unix()+5 {
			last = r
		}
	}
	if last.Time < now.Unix()-30 || len(last.Values) != len(doc.Legend) {
		return nil
	}
	out := map[string]rrdSample{}
	for i, name := range doc.Legend {
		f := strings.Split(name, ":")
		if len(f) != 4 || f[0] != "AVERAGE" || f[1] != "vm" {
			continue
		}
		id := strings.ToLower(f[2])
		s := out[id]
		v, e := strconv.ParseFloat(last.Values[i], 64)
		valid := e == nil && !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
		switch {
		case strings.HasPrefix(f[3], "cpu"):
			n, e := strconv.Atoi(strings.TrimPrefix(f[3], "cpu"))
			if e != nil || n < 0 {
				continue
			}
			if !valid || v > 1 {
				s.missingCPU = true
			} else {
				if s.cpu == nil {
					s.cpu = map[int]float64{}
				}
				s.cpu[n] = v
			}
		case valid && v < float64(math.MaxInt64) && f[3] == "memory":
			n := int64(v)
			s.memory = &n
		case valid && v < float64(math.MaxInt64) && f[3] == "memory_internal_free":
			n := int64(v)
			s.free = &n
		default:
			continue
		}
		out[id] = s
	}
	return out
}
