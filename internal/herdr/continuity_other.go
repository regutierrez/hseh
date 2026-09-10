//go:build !linux

package herdr

import (
	"fmt"
	"net"
)

func readContinuityWitnessFromUnixConn(*net.UnixConn) (ContinuityWitness, error) {
	return ContinuityWitness{}, fmt.Errorf("hseh history: continuity witness is only implemented on Linux")
}
