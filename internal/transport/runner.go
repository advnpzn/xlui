// Package transport runs noninteractive toolstack commands locally or over OpenSSH.
package transport

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

var destination = regexp.MustCompile(`^(ssh://)?([a-zA-Z0-9_][a-zA-Z0-9._-]*@)?([a-zA-Z0-9][a-zA-Z0-9._-]*|\[[a-fA-F0-9:]+\])$`)
var literalWord = regexp.MustCompile(`^[a-zA-Z0-9_./:@%+=,-]+$`)

type Runner struct {
	host string
}

func NewRunner(host string) (Runner, error) {
	if host != "" && !destination.MatchString(host) {
		return Runner{}, errors.New("invalid SSH destination; use an SSH config alias for custom ports or jump hosts")
	}
	return Runner{host: host}, nil
}

func (r Runner) Run(ctx context.Context, stdout, stderr io.Writer, program string, args ...string) error {
	var cmd *exec.Cmd
	if r.host == "" {
		cmd = exec.CommandContext(ctx, program, args...)
	} else {
		words := []string{"LC_ALL=C", shellWord(program)}
		for _, arg := range args {
			words = append(words, shellWord(arg))
		}
		cmd = exec.CommandContext(ctx, "ssh", "-T", "--", r.host, strings.Join(words, " "))
	}
	// An xl or SSH process has no use for credentials for a different provider.
	cmd.Env = slices.DeleteFunc(cmd.Environ(), func(entry string) bool {
		return strings.HasPrefix(entry, "XLUI_XAPI_PASSWORD=")
	})
	if r.host == "" {
		cmd.Env = append(cmd.Env, "LC_ALL=C")
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

// OpenSSH passes the remote command to a shell. Preserve each argument as one word.
func shellWord(word string) string {
	if literalWord.MatchString(word) {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}
