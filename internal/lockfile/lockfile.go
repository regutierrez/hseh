package lockfile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func WithExclusive(lockPath string, fn func() error) error {
	return WithExclusiveContext(context.Background(), lockPath, fn)
}

func retryable(err error) bool {
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK)
}

func WithExclusiveContext(ctx context.Context, lockPath string, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return fmt.Errorf("hseh lock: mkdir %s: %w", filepath.Dir(lockPath), err)
	}
	file, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("hseh lock: open %s: %w", lockPath, err)
	}
	defer file.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if !retryable(err) {
			return fmt.Errorf("hseh lock: flock %s: %w", lockPath, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}
