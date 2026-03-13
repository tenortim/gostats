//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// notifySIGHUP arranges for SIGHUP to be delivered to ch.
func notifySIGHUP(ch chan<- os.Signal) {
	signal.Notify(ch, syscall.SIGHUP)
}
