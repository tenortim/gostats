//go:build windows

package main

import "os"

// notifySIGHUP is a no-op on Windows; config reload via SIGHUP is not supported.
func notifySIGHUP(_ chan<- os.Signal) {}
