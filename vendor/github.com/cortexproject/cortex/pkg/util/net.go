package util

import (
	"fmt"
	"net"
)

// GetFirstAddressOf returns the first IPv4 address of the supplied interface names.
func GetFirstAddressOf(names []string) (string, error) {
	for _, name := range names {
		inf, err := net.InterfaceByName(name)
		if err != nil {
			continue
		}

		addrs, err := inf.Addrs()
		if err != nil {
			return "", err
		}
		if len(addrs) <= 0 {
			return "", fmt.Errorf("No address found for %s", name)
		}

		for _, addr := range addrs {
			switch v := addr.(type) {
			case *net.IPNet:
				if ip := v.IP.To4(); ip != nil {
					return v.IP.String(), nil
				}
			}
		}
	}

	return "", fmt.Errorf("No address found for %s", names)
}
