package main

import (
	"log/slog"
	"syscall"

	"golang.org/x/sys/windows"
)

// Control sets SO_REUSEADDR socket option on the listening socket.
func Control(network, address string, c syscall.RawConn) error {
	return c.Control(func(fd uintptr) {
		err := windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
		if err != nil {
			log.Warn("Could not set SO_REUSEADDR socket option", slog.String("error", err.Error()))
		}
	})
}
