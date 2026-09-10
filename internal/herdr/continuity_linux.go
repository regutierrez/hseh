//go:build linux

package herdr

import (
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"

	"github.com/regutierrez/hseh/internal/trace"
)

func readContinuityWitnessFromUnixConn(unixConn *net.UnixConn) (ContinuityWitness, error) {
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return ContinuityWitness{}, fmt.Errorf("hseh history: continuity witness control: %w", err)
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return ContinuityWitness{}, fmt.Errorf("hseh history: continuity witness control: %w", err)
	}
	if credErr != nil {
		return ContinuityWitness{}, fmt.Errorf("hseh history: continuity witness peer cred: %w", credErr)
	}
	if cred == nil || cred.Pid <= 0 {
		return ContinuityWitness{}, fmt.Errorf("hseh history: continuity witness missing peer pid")
	}
	startTime, err := trace.ProcStartTicks(int(cred.Pid))
	if err != nil {
		return ContinuityWitness{}, fmt.Errorf("hseh history: continuity witness: %w", err)
	}
	bootTime, err := readProcBootTime()
	if err != nil {
		return ContinuityWitness{}, err
	}
	return ContinuityWitness{
		PeerPID:       int(cred.Pid),
		PeerStartTime: startTime,
		BootTime:      bootTime,
	}, nil
}

func readProcBootTime() (string, error) {
	payload, err := os.ReadFile("/proc/stat")
	if err != nil {
		return "", fmt.Errorf("hseh history: continuity witness /proc/stat: %w", err)
	}
	for _, line := range strings.Split(string(payload), "\n") {
		if strings.HasPrefix(line, "btime ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "btime ")), nil
		}
	}
	return "", fmt.Errorf("hseh history: continuity witness /proc/stat missing btime")
}
