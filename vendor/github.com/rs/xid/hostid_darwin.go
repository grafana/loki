// +build darwin

package xid

import (
	"errors"
	"os/exec"
	"strings"
)

func readPlatformMachineID() (string, error) {
	ioreg, err := exec.LookPath("ioreg")
	if err != nil {
		return "", err
	}

	cmd := exec.Command(ioreg, "-rd1", "-c", "IOPlatformExpertDevice")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "IOPlatformUUID") {
			parts := strings.SplitAfter(line, `" = "`)
			if len(parts) == 2 {
				uuid := strings.TrimRight(parts[1], `"`)
				return strings.ToLower(uuid), nil
			}
		}
	}

	return "", errors.New("cannot find host id")
}
