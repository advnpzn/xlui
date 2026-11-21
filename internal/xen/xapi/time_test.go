package xapi

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBootTimeWireFormats(t *testing.T) {
	want := time.Date(2026, 1, 1, 10, 0, 0, 123456789, time.UTC)
	for _, raw := range []string{
		`"2026-01-01T10:00:00.123456789Z"`,
		`"20260101T10:00:00.123456789"`,
		`"2026-01-01T12:00:00.123456789+02"`,
		`"20260101T153000.123456789+0530"`,
		`"20260101T15:30:00.123456789+05:30"`,
		`1767261600.123456789`, `"1767261600.123456789"`, `1.767261600123456789e9`,
	} {
		if got := bootTime(json.RawMessage(raw)); !got.Equal(want) {
			t.Errorf("bootTime(%s) = %v, want %v", raw, got, want)
		}
	}
	for _, raw := range []string{"", `null`, `true`, `{}`, `""`, `"unknown"`, `"19700101T000000Z"`, `0`, `-1`, `1e10000000`, `1767261600.1234567891`} {
		if got := bootTime(json.RawMessage(raw)); !got.IsZero() {
			t.Errorf("bootTime(%s) unexpectedly usable: %v", raw, got)
		}
	}
}

func TestStartTimeCannotHideOtherMetrics(t *testing.T) {
	var records map[string]liveMetrics
	err := json.Unmarshal([]byte(`{
		"a":{"start_time":{"unsupported":"date"},"memory_actual":1048576,"VCPUs_number":2},
		"b":{"start_time":1767261600,"memory_actual":2097152,"VCPUs_number":4},
		"c":{"memory_actual":4194304,"VCPUs_number":1}
	}`), &records)
	if err != nil {
		t.Fatal(err)
	}
	for ref, mem := range map[string]string{"a": "1048576", "b": "2097152", "c": "4194304"} {
		r := records[ref]
		if r.Memory.String() != mem || r.Count == "" || bootTime(r.Start).IsZero() != (ref != "b") {
			t.Errorf("incorrect metrics for %s: %+v", ref, r)
		}
	}
}
