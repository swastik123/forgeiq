package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Slot struct {
	ID      string
	BaseDir string // .../slots/slot-N
	lockF   *os.File
}

type SlotPool struct {
	BaseDir string // /var/lib/forgeiq-sbx
	Slots   int
}

func (p SlotPool) EnsureLayout() error {
	if strings.TrimSpace(p.BaseDir) == "" {
		return fmt.Errorf("base dir required")
	}
	if p.Slots <= 0 {
		p.Slots = 2
	}
	// create base structure
	for _, sub := range []string{"rootfs", "runs", "slots"} {
		if err := os.MkdirAll(filepath.Join(p.BaseDir, sub), 0o755); err != nil {
			return err
		}
	}
	// slot directories
	for i := 1; i <= p.Slots; i++ {
		sd := filepath.Join(p.BaseDir, "slots", fmt.Sprintf("slot-%d", i))
		for _, sub := range []string{"workspace", "out", "bundle", "state"} {
			if err := os.MkdirAll(filepath.Join(sd, sub), 0o755); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p SlotPool) Acquire(timeout time.Duration) (*Slot, error) {
	if err := p.EnsureLayout(); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		for i := 1; i <= p.Slots; i++ {
			sd := filepath.Join(p.BaseDir, "slots", fmt.Sprintf("slot-%d", i))
			lf := filepath.Join(sd, ".lock")
			f, err := os.OpenFile(lf, os.O_CREATE|os.O_RDWR, 0o600)
			if err != nil {
				continue
			}
			if tryLock(f) {
				return &Slot{ID: fmt.Sprintf("slot-%d", i), BaseDir: sd, lockF: f}, nil
			}
			_ = f.Close()
		}
		if timeout <= 0 || time.Now().After(deadline) {
			return nil, fmt.Errorf("no free sandbox slots")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (s *Slot) Release() {
	if s == nil || s.lockF == nil {
		return
	}
	_ = unlock(s.lockF)
	_ = s.lockF.Close()
	s.lockF = nil
}

func tryLock(f *os.File) bool {
	// Best-effort flock. Works on Linux/Darwin.
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	return err == nil
}

func unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

func (s *Slot) RunDir(runID string) string {
	// Keep run dirs separate for debugging TTL.
	return filepath.Join(s.BaseDir, "out", "runs", sanitize(runID))
}

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, " ", "_")
	if s == "" {
		return "run-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	// Avoid huge names on some filesystems
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

func (s *Slot) DebugInfo() map[string]any {
	return map[string]any{
		"slot_id":  s.ID,
		"base_dir": s.BaseDir,
		"os":       runtime.GOOS,
	}
}

