//go:build linux
// +build linux

package main

import (
	"fmt"
	"log/slog"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	TCP_FASTOPEN         = 23 // linux/include/uapi/linux/tcp.h
	TCP_FASTOPEN_CONNECT = 30 // linux/include/uapi/linux/tcp.h
)

func SetQuickAck(conn net.Conn) error {
	tcpconn, ok := conn.(*net.TCPConn)
	if !ok {
		return fmt.Errorf("failed type assertion %q", conn.RemoteAddr())
	}
	syscon, err := tcpconn.SyscallConn()
	if err != nil {
		return fmt.Errorf("could not get syscall conn %q: %w", conn.RemoteAddr(), err)
	}
	err = syscon.Control(func(fd uintptr) {
		err := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_QUICKACK, 1)
		if err != nil {
			slog.Error("failed to set TCP_QUICKACK", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("could not control %q: %w", conn.RemoteAddr(), err)
	}
	return nil
}

func GetTCPControlWithFastOpen() func(network, address string, c syscall.RawConn) error {
	return func(network, _ string, c syscall.RawConn) error {
		return c.Control(func(fd uintptr) {
			var err error
			// Enable REUSEDPORT on the server side
			err = syscall.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
			if err != nil {
				slog.Error("failed to set SO_REUSEPORT", "error", err)
			}
			// Enable FASTOPEN on the client side
			err = syscall.SetsockoptInt(int(fd), syscall.SOL_TCP, TCP_FASTOPEN_CONNECT, 1)
			if err != nil {
				slog.Error("failed to set TCP_FASTOPEN_CONNECT", "error", err)
			}
			// Enable FASTOPEN on the server side
			err = syscall.SetsockoptInt(int(fd), syscall.SOL_TCP, TCP_FASTOPEN, 1)
			if err != nil {
				slog.Error("failed to set TCP_FASTOPEN", "error", err)
			}
		})
	}
}
