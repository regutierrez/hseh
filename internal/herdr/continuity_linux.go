//go:build linux

package herdr

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
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
	startTime, err := readProcStartTime(int(cred.Pid))
	if err != nil {
		return ContinuityWitness{}, err
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

func readProcStartTime(pid int) (string, error) {
	payload, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", fmt.Errorf("hseh history: continuity witness /proc/%d/stat: %w", pid, err)
	}
	text := string(payload)
	closeParen := strings.LastIndex(text, ")")
	if closeParen < 0 || closeParen+2 >= len(text) {
		return "", fmt.Errorf("hseh history: continuity witness /proc/%d/stat missing comm", pid)
	}
	fields := strings.Fields(text[closeParen+2:])
	if len(fields) < 20 {
		return "", fmt.Errorf("hseh history: continuity witness /proc/%d/stat too short", pid)
	}
	return fields[19], nil
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
