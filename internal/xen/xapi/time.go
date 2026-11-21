package xapi

import (
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// XenAPI accepts ISO 8601 variants and Unix seconds. An unreadable timestamp
// disables instance-bound actions without discarding unrelated pool metrics.
func bootTime(raw json.RawMessage) time.Time {
	if len(raw) == 0 || len(raw) > 128 {
		return time.Time{}
	}
	s := string(raw)
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &s); err != nil {
			return time.Time{}
		}
	}
	date, clock, iso := strings.Cut(s, "T")
	if !iso {
		return unixBootTime(s)
	}
	dateLayout := "20060102"
	if strings.Contains(date, "-") {
		dateLayout = "2006-01-02"
	}
	zone := ""
	if i := strings.IndexAny(clock, "Z+-"); i >= 0 {
		zone, clock = clock[i:], clock[:i]
	}
	if i := strings.IndexAny(clock, ".,"); i >= 0 && len(clock)-i-1 > 9 {
		return time.Time{} // A truncated timestamp cannot identify an instance.
	}
	clockLayout := "150405"
	if strings.Contains(clock, ":") {
		clockLayout = "15:04:05"
	}
	var zoneLayout string
	switch len(zone) {
	case 0:
	case 1, 6:
		zoneLayout = "Z07:00"
	case 3:
		zoneLayout = "Z07"
	case 5:
		zoneLayout = "Z0700"
	default:
		return time.Time{}
	}
	t, err := time.Parse(dateLayout+"T"+clockLayout+zoneLayout, s)
	if err != nil || !t.After(time.Unix(0, 0)) {
		return time.Time{}
	}
	return t.UTC()
}

func unixBootTime(s string) time.Time {
	if len(s) == 0 || s[0] < '0' || s[0] > '9' || !json.Valid([]byte(s)) {
		return time.Time{}
	}
	// Bound exponent expansion before parsing an untrusted decimal exactly.
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exp, err := strconv.Atoi(s[i+1:])
		if err != nil || exp < -128 || exp > 128 {
			return time.Time{}
		}
	}
	seconds, ok := new(big.Rat).SetString(s)
	if !ok || seconds.Sign() <= 0 {
		return time.Time{}
	}
	nanos := seconds.Mul(seconds, big.NewRat(1e9, 1))
	if !nanos.IsInt() {
		return time.Time{}
	}
	var sec, ns big.Int
	sec.QuoRem(nanos.Num(), big.NewInt(1e9), &ns)
	if !sec.IsInt64() || sec.Int64() > 253402300799 { // End of ISO year 9999.
		return time.Time{}
	}
	return time.Unix(sec.Int64(), ns.Int64()).UTC()
}
