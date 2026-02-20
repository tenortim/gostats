package main

import (
	"fmt"
	"log/slog"
	"net"
	"strings"
)

// IsExternalInterface uses a string prefix list to weed out known internal interface names
func IsExternalInterface(ifname string) bool {
	switch {
	case strings.HasPrefix(ifname, "docker"):
		return false
	case strings.HasPrefix(ifname, "lxdbr"):
		return false
	default:
		return true
	}
}

// IsIPv4 returns true if the address is IPv4, false if IPv6
// This is a simple check based on the number of colons in the address
// It will not catch all cases, but should be sufficient for our needs
func IsIPv4(address string) bool {
	return strings.Count(address, ":") < 2
}

// IsIPv6 returns true if the address is IPv6, false if IPv4
// This is a simple check based on the number of colons in the address
// It will not catch all cases, but should be sufficient for our needs
func IsIPv6(address string) bool {
	return strings.Count(address, ":") >= 2
}

// ListExternalIPs returns a list of IP addresses on externally-reachable interfaces
func ListExternalIPs() ([]net.IP, error) {
	var ips []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate network interfaces: %w", err)
	}
	for _, i := range ifaces {
		if !IsExternalInterface(i.Name) {
			log.Debug("skipping internal interface", slog.String("interface", i.Name))
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			return nil, fmt.Errorf("failed to enumerate network addresses: %w", err)
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			default:
				continue
			}
			if ip.IsGlobalUnicast() {
				ips = append(ips, ip)
			}
		}
	}
	return ips, nil
}

// findExternalAddr attempt to find a reachable external IP address for the system
func findExternalAddr() (string, error) {
	// Discover local (listener) IP address
	// Prefer IPv4 addresses
	// If multiple are found default to the first
	var listenAddr string

	ips, err := ListExternalIPs()
	if err != nil {
		return "", fmt.Errorf("unable to list external IP addresses: %w", err)
	}
	for _, ip := range ips {
		if IsIPv4(ip.String()) {
			listenAddr = ip.String()
			break
		}
	}
	if listenAddr == "" {
		// No IPv4 addresses found, choose the first IPv6 address
		if len(ips) == 0 {
			return "", fmt.Errorf("no valid external IP addresses found")
		}
		listenAddr = ips[0].String()
	}
	return listenAddr, nil
}
