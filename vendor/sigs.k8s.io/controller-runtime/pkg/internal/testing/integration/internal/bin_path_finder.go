package internal

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var assetsPath string

func init() {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("Could not determine the path of the BinPathFinder")
	}
	assetsPath = filepath.Join(filepath.Dir(thisFile), "..", "assets", "bin")
}

// BinPathFinder checks the an environment variable, derived from the symbolic name,
// and falls back to a default assets location when this variable is not set
func BinPathFinder(symbolicName string) (binPath string) {
	punctuationPattern := regexp.MustCompile("[^A-Z0-9]+")
	sanitizedName := punctuationPattern.ReplaceAllString(strings.ToUpper(symbolicName), "_")
	leadingNumberPattern := regexp.MustCompile("^[0-9]+")
	sanitizedName = leadingNumberPattern.ReplaceAllString(sanitizedName, "")
	envVar := "TEST_ASSET_" + sanitizedName

	if val, ok := os.LookupEnv(envVar); ok {
		return val
	}

	return filepath.Join(assetsPath, symbolicName)
}
