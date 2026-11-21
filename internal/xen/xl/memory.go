package xl

import (
	"context"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/advnpzn/xlui/internal/xen"
)

// ReadMonitor augments xl's allocation with guest-reported usage where tools
// publish it. Only xl supplies inventory. Optional probes cannot discard it.
func ReadMonitor(ctx context.Context, runner Runner) ([]xen.Domain, error) {
	domains, err := ReadDomains(ctx, runner)
	if err != nil || len(domains) == 0 {
		return domains, err
	}
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	args := []string{"-c", guestMemoryProbe, "xlui-memory"}
	for _, d := range domains {
		args = append(args, strconv.Itoa(d.ID))
	}
	out := cappedOutput{limit: 1 << 20}
	// Each complete line is independently verified, including after a timeout.
	_ = runner.Run(probe, &out, io.Discard, "sh", args...)
	applyGuestMemory(domains, out.buf.String())
	return domains, nil
}

// The script is fixed; the only positional arguments are parsed numeric IDs.
// Multi-key xenstore-read uses one transaction for the VM identity and counters.
const guestMemoryProbe = `for id do
	if [ "$id" = 0 ]; then
		awk '/^MemTotal:/ {t=$2} /^MemAvailable:/ {a=$2} END {if(t && a!="") print "0 /vm/00000000-0000-0000-0000-000000000000", t, a}' /proc/meminfo
		continue
	fi
	command -v xenstore-read >/dev/null 2>&1 || continue
	p=/local/domain/$id
	values=$(xenstore-read "$p/vm" "$p/data/meminfo_total" "$p/data/meminfo_free" 2>/dev/null) || continue
	# Numbers and a single /vm/UUID are the only accepted tokens.
	set -f
	set -- $values
	[ "$#" = 3 ] || continue
	printf '%s %s %s %s\n' "$id" "$1" "$2" "$3"
done`

func applyGuestMemory(domains []xen.Domain, output string) {
	byID := make(map[int]*xen.Domain, len(domains))
	for i := range domains {
		byID[domains[i].ID] = &domains[i]
	}
	for {
		line, rest, ok := strings.Cut(output, "\n")
		if !ok {
			return
		}
		output = rest
		f := strings.Fields(line)
		if len(f) != 4 {
			continue
		}
		id, e := strconv.Atoi(f[0])
		d := byID[id]
		if e != nil || d == nil || !strings.EqualFold(f[1], "/vm/"+d.UUID) {
			continue
		}
		total, e1 := strconv.ParseInt(f[2], 10, 64)
		free, e2 := strconv.ParseInt(f[3], 10, 64)
		if e1 == nil && e2 == nil && total > 0 && free >= 0 && free <= total {
			used := total - free
			d.GuestUsedKiB = &used
		}
	}
}
