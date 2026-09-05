//go:build darwin

package term

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// macOS has no /proc. What it has is ps, which names the parent's terminal
// as `ttys003`, a device under /dev that is both where the size is read and
// where a picture goes. Empty when the parent has none, which is every
// oracle and every headless session.
//
// Written blind: nothing in the fleet runs macOS. The width falling to
// COLUMNS and then 100 is the failure if this is wrong, not a crash.
func parentTTY() string {
	out, err := exec.Command("ps", "-o", "tty=", "-p", strconv.Itoa(os.Getppid())).Output()
	t := strings.TrimSpace(string(out))
	if err != nil || t == "" || t == "??" {
		return ""
	}
	return "/dev/" + t
}

func TTYOut() string { return parentTTY() }

// The parent's environment is not readable here: macOS has no /proc, and
// ps does not carry it. A hook whose own TERM was scrubbed is blind, which
// is the known gap the README already owns.
func parentEnv(string) string { return "" }
