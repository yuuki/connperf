package sock

import (
	"net"

	"golang.org/x/sys/unix"
	"golang.org/x/xerrors"
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
		unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_QUICKACK, 1)
	})
	if err != nil {
		return xerrors.Errorf("Could not control %q: %w", conn.RemoteAddr(), err)
	}
	return nil
}
