//go:build !darwin

package term

import (
	"os"
	"strconv"
	"strings"
)

// The parent's terminal, through /proc: its stdin is where the size is
// read, its stdout is where a picture goes. Both are the same tty in an
// interactive session and neither exists in an oracle.
func parentTTY() string { return "/proc/" + strconv.Itoa(os.Getppid()) + "/fd/0" }

// TTYOut is where a picture is written: the parent's stdout, which is the
// terminal itself in an interactive session and nothing in an oracle.
func TTYOut() string { return "/proc/" + strconv.Itoa(os.Getppid()) + "/fd/1" }

// parentEnv is one variable of the parent's environment, read the same
// way. What CC scrubs out of a hook's own environment, `claude` still
// carries.
func parentEnv(key string) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(os.Getppid()) + "/environ")
	if err != nil {
		return ""
	}
	for _, kv := range strings.Split(string(b), "\x00") {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}
