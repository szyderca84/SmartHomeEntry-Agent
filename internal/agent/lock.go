package agent

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

const lockFilePath = "/var/run/smarthomeentry-agent.pid"

// acquireLock obtains an exclusive, non-blocking flock on the PID file.
// It returns the open file handle (must be closed on exit to release the lock)
// or an error if another instance is already running.
func acquireLock() (*os.File, error) {
	f, err := os.OpenFile(lockFilePath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", lockFilePath, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another instance is already running (lock: %s)", lockFilePath)
		}
		return nil, fmt.Errorf("acquire flock on %s: %w", lockFilePath, err)
	}

	// Record PID so operators can inspect the running process.
	if err := f.Truncate(0); err == nil {
		_, _ = f.Seek(0, 0)
		_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	}

	return f, nil
}
