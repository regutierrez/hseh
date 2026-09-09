//go:build !linux

package herdr

import (
	"fmt"
	"net"
)

func readContinuityWitnessFromUnixConn(unixConn *net.UnixConn) (ContinuityWitness, error) {
	_ = unixConn
	return ContinuityWitness{}, fmt.Errorf("hseh history: continuity witness is only implemented on Linux")
}
