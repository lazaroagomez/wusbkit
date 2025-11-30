package lock

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// DiskLock provides exclusive access to a disk across multiple process instances
type DiskLock struct {
	diskNumber int
	lock       *flock.Flock
	lockPath   string
}

// NewDiskLock creates a lock for the specified disk number
func NewDiskLock(diskNumber int) (*DiskLock, error) {
	lockDir := filepath.Join(os.TempDir(), "wusbkit-locks")
	if err := os.MkdirAll(lockDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}

	lockPath := filepath.Join(lockDir, fmt.Sprintf("disk-%d.lock", diskNumber))
	return &DiskLock{
		diskNumber: diskNumber,
		lock:       flock.New(lockPath),
		lockPath:   lockPath,
	}, nil
}

// TryLock attempts to acquire the lock with a timeout
func (d *DiskLock) TryLock(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	locked, err := d.lock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		// Context timeout means another process holds the lock
		if err == context.DeadlineExceeded {
			return fmt.Errorf("disk %d is being used by another wusbkit instance", d.diskNumber)
		}
		return fmt.Errorf("lock error: %w", err)
	}
	if !locked {
		return fmt.Errorf("disk %d is being used by another wusbkit instance", d.diskNumber)
	}
	return nil
}

// Unlock releases the lock
func (d *DiskLock) Unlock() error {
	if d.lock == nil {
		return nil
	}
	return d.lock.Unlock()
}
