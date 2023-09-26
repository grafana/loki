package net

import (
	"fmt"
	"net"
)

// LoopbackInterfaceName search for the name of a loopback interface in the list
// of the system's network interfaces and returns the first one found.
func LoopbackInterfaceName() (string, error) {
	is, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("can't retrieve loopback interface name: %s", err)
	}

	for _, i := range is {
		if i.Flags&net.FlagLoopback != 0 {
			return i.Name, nil
		}
	}

	return "", fmt.Errorf("can't retrieve loopback interface name")
}
