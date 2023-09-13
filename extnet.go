package main

import (
	"fmt"
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

func IsIPv4(address string) bool {
	return strings.Count(address, ":") < 2
}

func IsIPv6(address string) bool {
	return strings.Count(address, ":") >= 2
}

// ListExternalIPs returns a list of IP addresses on externally-reachable interfaces
func ListExternalIPs() ([]net.IP, error) {
	var ips []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate network interfaces: %v", err)
	}
	for _, i := range ifaces {
		if !IsExternalInterface(i.Name) {
			log.Debugf("skipping internal interface %s", i.Name)
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			return nil, fmt.Errorf("failed to enumerate network addresses: %v", err)
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
