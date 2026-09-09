package herdr

import (
	"fmt"
	"net"

	"github.com/regutierrez/hseh/internal/config"
)

// ContinuityWitness proves the Herdr server process is the same one that
// wrote history. Matching labels or socket paths are not continuity.
type ContinuityWitness struct {
	SocketPath    string `json:"socket_path"`
	PeerPID       int    `json:"peer_pid"`
	PeerStartTime string `json:"peer_start_time"`
	BootTime      string `json:"boot_time,omitempty"`
}

func SameContinuityWitness(stored, live ContinuityWitness) bool {
	return stored.SocketPath != "" &&
		stored.SocketPath == live.SocketPath &&
		stored.PeerPID != 0 &&
		stored.PeerPID == live.PeerPID &&
		stored.PeerStartTime != "" &&
		stored.PeerStartTime == live.PeerStartTime &&
		stored.BootTime == live.BootTime
}

// ReadContinuityWitnessFromConn reads peer identity from the live API connection.
func ReadContinuityWitnessFromConn(conn net.Conn, socketPath string) (ContinuityWitness, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return ContinuityWitness{}, fmt.Errorf("hseh history: continuity witness requires a unix socket")
	}
	witness, err := readContinuityWitnessFromUnixConn(unixConn)
	if err != nil {
		return ContinuityWitness{}, err
	}
	witness.SocketPath = socketPath
	return witness, nil
}

// ReadContinuityWitness opens a socket only when no in-flight API conn exists.
func ReadContinuityWitness() (ContinuityWitness, error) {
	socketPath := config.SocketPath()
	if socketPath == "" {
		return ContinuityWitness{}, fmt.Errorf("hseh history: HERDR_SOCKET_PATH is not set")
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return ContinuityWitness{}, fmt.Errorf("hseh history: dial for continuity witness: %w", err)
	}
	defer conn.Close()
	return ReadContinuityWitnessFromConn(conn, socketPath)
}
