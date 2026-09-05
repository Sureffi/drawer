// hookstate.go — what one delta's process leaves for the next.
//
// A fence does not arrive whole. CC splits a streamed reply into deltas at
// boundaries it does not promise and does not repeat: the same prompt gave
// ["```dot\n<source>\n", "```"] on one run and ["```dot\n", "<source>\n",
// "```"] on the next. A hook is one process per delta, so the transducer
// that reassembles the fence (internal/fence) has to keep its half-finished
// work somewhere the next process can find it: a file per message id.
//
// And the processes are not one after another. Measured: the processes
// for a reply's two deltas started eleven microseconds apart and ran side
// by side, so the second read an empty state, saw no fence open and gave
// its half of the source back raw, while the first opened a fence nothing
// ever closed. So a process takes its turn: the state carries the index
// of the delta it expects next, a lock file per message serialises the
// readers, and a process whose delta is ahead of the count waits for the
// one before it — a few milliseconds, the draw included — before it reads.
// A turn nobody takes is waited for only so long, then taken anyway,
// which is the old behaviour and its old hazard.

package drawer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sureffi/drawer/internal/fence"
)

// stateDir is where the hook keeps what one process leaves for the next.
func stateDir() string {
	dir := os.Getenv("DRAWER_STATE")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "drawer")
	}
	os.MkdirAll(dir, 0o700)
	return dir
}

// safeName keeps of an id only what cannot escape a directory. Message and
// session ids are uuids from CC.
func safeName(id string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '_'
	}, id)
}

func statePath(msgID string) string {
	return filepath.Join(stateDir(), safeName(msgID)+".json")
}

// sweepState drops files older than `age` — state left by turns that ended
// without a final delta: an abort mid-fence writes a file nothing will ever
// come back for. The cost of dropping a live one is a fence that shows its
// source, which is the failure this whole file is built to fall back to
// anyway. Directories are somebody else's and are left alone.
func sweepState(dir string, age time.Duration) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		info, err := e.Info()
		if err != nil || e.IsDir() || time.Since(info.ModTime()) < age {
			continue
		}
		os.Remove(filepath.Join(dir, e.Name()))
	}
}

func loadState(msgID string) fence.State {
	var s fence.State
	p := statePath(msgID)
	sweepState(filepath.Dir(p), 10*time.Minute)
	b, err := os.ReadFile(p)
	if err != nil {
		return s
	}
	json.Unmarshal(b, &s)
	return s
}

// saveState keeps the state for the next delta's process, or at the
// message's final delta takes it away, lock and all.
func saveState(msgID string, s fence.State, final bool) {
	if final {
		os.Remove(statePath(msgID))
		os.Remove(lockPath(msgID))
		return
	}
	b, _ := json.Marshal(s)
	os.WriteFile(statePath(msgID), b, 0o600)
}

func lockPath(msgID string) string { return statePath(msgID) + ".lock" }

// turnPatience is how long a process waits for the delta before its own.
// Claude Code allows a hook far longer, and a turn nobody is coming to
// take should not hold a reply that long.
const turnPatience = 2 * time.Second

// takeTurn waits for a delta's turn on its message and takes it: the
// state as the process before left it, under the message's lock, which
// the caller holds through the draw and releases with done. A delta the
// state has already counted past — a repeat — takes its turn at once.
func takeTurn(msgID string, index int, patience time.Duration) (fence.State, func()) {
	f, err := os.OpenFile(lockPath(msgID), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return loadState(msgID), func() {}
	}
	deadline := time.Now().Add(patience)
	for {
		syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		st := loadState(msgID)
		if index <= st.Next || !time.Now().Before(deadline) {
			return st, func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }
		}
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		time.Sleep(3 * time.Millisecond)
	}
}
