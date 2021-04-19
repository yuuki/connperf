// +build !linux

package sock

import (
	"net"
	"syscall"
)

func SetQuickAck(conn net.Conn) error {
	return nil
}

func SetLinger(conn net.Conn) error {
	return nil
}

func GetTCPControlWithFastOpen() func(network, address string, c syscall.RawConn) error {
	return func(network, _ string, c syscall.RawConn) error {
		return nil
	}
}
