package main

import (
	"syscall"
	"testing"
)

func TestRetryExclusiveLockErrorOnlyContention(t *testing.T) {
	if !retryExclusiveLockError(syscall.EAGAIN) {
		t.Fatal("EAGAIN should retry")
	}
	if !retryExclusiveLockError(syscall.EWOULDBLOCK) {
		t.Fatal("EWOULDBLOCK should retry")
	}
	if retryExclusiveLockError(syscall.EPERM) {
		t.Fatal("EPERM must not retry")
	}
	if retryExclusiveLockError(syscall.EINVAL) {
		t.Fatal("EINVAL must not retry")
	}
	if retryExclusiveLockError(syscall.EINTR) {
		t.Fatal("EINTR is handled separately, not as contention")
	}
}
