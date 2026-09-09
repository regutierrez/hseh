package lockfile

import (
	"syscall"
	"testing"
)

func TestRetryExclusiveLockErrorOnlyContention(t *testing.T) {
	if !retryable(syscall.EAGAIN) {
		t.Fatal("EAGAIN should retry")
	}
	if !retryable(syscall.EWOULDBLOCK) {
		t.Fatal("EWOULDBLOCK should retry")
	}
	if retryable(syscall.EPERM) {
		t.Fatal("EPERM must not retry")
	}
	if retryable(syscall.EINVAL) {
		t.Fatal("EINVAL must not retry")
	}
	if retryable(syscall.EINTR) {
		t.Fatal("EINTR is handled separately, not as contention")
	}
}
