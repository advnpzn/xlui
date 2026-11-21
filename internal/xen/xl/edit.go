package xl

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/advnpzn/xlui/internal/xen"
)

// Keep existing comments, boot devices and site-specific fields byte-for-byte.
// Accept independent, single-line xl assignments. More complex definitions
// require a full source-preserving xl parser and are left to the host editor.
var assignment = regexp.MustCompile(`^[ \t]*([A-Za-z_][A-Za-z_0-9]*)[ \t]*=[ \t]*(.*)$`)
var decimalField = regexp.MustCompile(`^([1-9][0-9]*)[ \t\r]*(?:#.*)?$`)
var identityField = regexp.MustCompile(`^(['"])([A-Za-z0-9_.-]+)(['"])[ \t\r]*(?:#.*)?$`)
var errComplex = errors.New("edit requires one xl assignment per line, a literal name and UUID, and an explicit decimal resource value")

func independentLine(s string) bool {
	var quote rune
	depth := 0
	escaped := false
	for _, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if quote != 0 {
			if r == '\\' {
				escaped = true
			} else if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '#':
			return depth == 0
		case '\'', '"':
			quote = r
		case '[':
			depth++
		case ']':
			depth--
			if depth < 0 {
				return false
			}
		case ';', '\\':
			return false
		}
	}
	return quote == 0 && depth == 0
}

func editBoot(raw string, c xen.Change) (uuid, next, summary string, err error) {
	field, label, value := "memory", "Boot memory (MiB)", c.MemoryMiB
	switch c.Action {
	case xen.SetVCPUs:
		field, label, value = "vcpus", "Startup vCPUs", c.VCPUs
	case xen.SetMemory:
	default:
		return "", "", "", errors.New("unsupported guest edit")
	}
	if value < 1 || (field == "memory" && value > 1<<40) || (field == "vcpus" && value > 8192) {
		return "", "", "", errors.New("resource value is outside the supported range")
	}
	lines := strings.Split(raw, "\n")
	var name string
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		match := assignment.FindStringSubmatchIndex(line)
		if match == nil || !independentLine(line[match[4]:]) {
			return "", "", "", errComplex
		}
		key, arg := line[match[2]:match[3]], line[match[4]:]
		switch key {
		case "name", "uuid":
			id := identityField.FindStringSubmatch(arg)
			if id == nil || id[1] != id[3] {
				return "", "", "", errComplex
			}
			if key == "name" {
				if name != "" {
					return "", "", "", errComplex
				}
				name = id[2]
			} else {
				if uuid != "" {
					return "", "", "", errComplex
				}
				uuid = id[2]
			}
		case field:
			n := decimalField.FindStringSubmatch(arg)
			if n == nil || summary != "" {
				return "", "", "", errComplex
			}
			old, e := strconv.ParseInt(n[1], 10, 64)
			if e != nil {
				return "", "", "", errComplex
			}
			if old == value {
				return "", "", "", errors.New("the proposed value already matches the definition")
			}
			summary = fmt.Sprintf("%s: %d → %d", label, old, value)
			lines[i] = line[:match[4]] + strconv.FormatInt(value, 10) + arg[len(n[1]):]
		}
	}
	if !xen.GuestUUID(uuid) || name != c.Name || (c.Target.UUID != "" && !strings.EqualFold(uuid, c.Target.UUID)) {
		return "", "", "", errors.New("the definition's name and explicit UUID must match the target guest")
	}
	if summary == "" {
		return "", "", "", errComplex
	}
	return uuid, strings.Join(lines, "\n"), summary, nil
}
