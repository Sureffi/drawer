//go:build !darwin

package drawer

import (
	"os"
	"strconv"
)

// The parent's terminal, through /proc: its stdin is where the size is
// read, its stdout is where a picture goes. Both are the same tty in an
// interactive session and neither exists in an oracle.
func parentTTY() string    { return "/proc/" + strconv.Itoa(os.Getppid()) + "/fd/0" }
func parentTTYOut() string { return "/proc/" + strconv.Itoa(os.Getppid()) + "/fd/1" }
