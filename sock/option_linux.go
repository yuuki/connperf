// +build linux

package sock

import (
	"log"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.org/x/xerrors"
)

const (
	TCP_FASTOPEN         = 23 // linux/include/uapi/linux/tcp.h
	TCP_FASTOPEN_CONNECT = 30 // linux/include/uapi/linux/tcp.h
)

func SetQuickAck(conn net.Conn) error {
	tcpconn, ok := conn.(*net.TCPConn)
	if !ok {
		return xerrors.Errorf("failed type assertion %q", conn.RemoteAddr())
	}
	syscon, err := tcpconn.SyscallConn()
	if err != nil {
		return xerrors.Errorf("Could not get syscall conn %q: %w", conn.RemoteAddr(), err)
	}
	err = syscon.Control(func(fd uintptr) {
		err := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_QUICKACK, 1)
		if err != nil {
			log.Println(err)
		}
	})
	if err != nil {
		return xerrors.Errorf("Could not control %q: %w", conn.RemoteAddr(), err)
	}
	return nil
}

func SetLinger(conn net.Conn) error {
	tcpconn, ok := conn.(*net.TCPConn)
	if !ok {
		return xerrors.Errorf("failed type assertion %q", conn.RemoteAddr())
	}
	if err := tcpconn.SetLinger(0); err != nil {
		return xerrors.Errorf("could not set SO_LINGER: %w", err)
	}
	return nil
}

func GetTCPControlWithFastOpen() func(network, address string, c syscall.RawConn) error {
	return func(network, _ string, c syscall.RawConn) error {
		return c.Control(func(fd uintptr) {
			var err error
			// Enable FASTOPEN on the client side
			err = syscall.SetsockoptInt(int(fd), syscall.SOL_TCP, TCP_FASTOPEN_CONNECT, 1)
			if err != nil {
				log.Println(err)
			}
			// Enable FASTOPEN on the server side
			err = syscall.SetsockoptInt(int(fd), syscall.SOL_TCP, TCP_FASTOPEN, 1)
			if err != nil {
				log.Println(err)
			}
		})
	}
}
