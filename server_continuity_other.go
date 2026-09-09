//go:build !linux

package main

import (
	"fmt"
	"net"
)

func readContinuityWitnessFromUnixConn(unixConn *net.UnixConn) (ServerContinuityWitness, error) {
	_ = unixConn
	return ServerContinuityWitness{}, fmt.Errorf("hseh history: continuity witness is only implemented on Linux")
}
