// +build windows

package xid

import (
	"fmt"
	"syscall"
	"unsafe"
)

func readPlatformMachineID() (string, error) {
	// source: https://github.com/shirou/gopsutil/blob/master/host/host_syscall.go
	var h syscall.Handle

	regKeyCryptoPtr, err := syscall.UTF16PtrFromString(`SOFTWARE\Microsoft\Cryptography`)
	if err != nil {
		return "", fmt.Errorf(`error reading registry key "SOFTWARE\Microsoft\Cryptography": %w`, err)
	}

	err = syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, regKeyCryptoPtr, 0, syscall.KEY_READ|syscall.KEY_WOW64_64KEY, &h)
	if err != nil {
		return "", err
	}
	defer func() { _ = syscall.RegCloseKey(h) }()

	const syscallRegBufLen = 74 // len(`{`) + len(`abcdefgh-1234-456789012-123345456671` * 2) + len(`}`) // 2 == bytes/UTF16
	const uuidLen = 36

	var regBuf [syscallRegBufLen]uint16
	bufLen := uint32(syscallRegBufLen)
	var valType uint32

	mGuidPtr, err := syscall.UTF16PtrFromString(`MachineGuid`)
	if err != nil {
		return "", fmt.Errorf("error reading machine GUID: %w", err)
	}

	err = syscall.RegQueryValueEx(h, mGuidPtr, nil, &valType, (*byte)(unsafe.Pointer(&regBuf[0])), &bufLen)
	if err != nil {
		return "", fmt.Errorf("error parsing ")
	}

	hostID := syscall.UTF16ToString(regBuf[:])
	hostIDLen := len(hostID)
	if hostIDLen != uuidLen {
		return "", fmt.Errorf("HostID incorrect: %q\n", hostID)
	}

	return hostID, nil
}
