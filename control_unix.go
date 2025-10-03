//go:build !windows

package main

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func Control(network, address string, c syscall.RawConn) error {
	return c.Control(func(fd uintptr) {
		err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
		if err != nil {
			log.Warningf("Could not set SO_REUSEADDR socket option: %s", err)
		}
		err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		if err != nil {
			log.Warningf("Could not set SO_REUSEPORT socket option: %s", err)
		}
	})
}
