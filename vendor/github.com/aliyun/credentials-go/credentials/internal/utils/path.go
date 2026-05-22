package utils

import (
	"os"
	"runtime"
)

var getOS = func() string {
	return runtime.GOOS
}

func GetHomePath() string {
	if getOS() == "windows" {
		return os.Getenv("USERPROFILE")
	}

	return os.Getenv("HOME")
}
