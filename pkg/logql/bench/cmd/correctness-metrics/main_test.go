package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// resetFlags resets the flag package for test isolation.
func resetFlags() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
}

func writeTempXML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "results.xml")
	err := os.WriteFile(path, []byte(content), 0644)
	assert.NoError(t, err)
	return path
}

const sampleXML = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="pkg/logql/bench" tests="2" failures="1" time="5.0">
    <testcase name="TestRemoteStorageEquality/fast/basic.yaml:3/kind=metric/direction=FORWARD" time="2.0"></testcase>
    <testcase name="TestRemoteStorageEquality/fast/basic.yaml:3/kind=log/direction=FORWARD" time="3.0">
      <failure message="mismatch">values differ</failure>
    </testcase>
  </testsuite>
</testsuites>`

// setArgs sets os.Args for the test and registers cleanup to restore the original value.
func setArgs(t *testing.T, args []string) {
	t.Helper()
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })
	os.Args = args
}

func TestRun_DryRun(t *testing.T) {
	resetFlags()
	xmlPath := writeTempXML(t, sampleXML)
	setArgs(t, []string{"correctness-metrics", "--input", xmlPath, "--range-type", "range", "--dry-run"})
	assert.Equal(t, 0, run())
}

func TestRun_MissingInput(t *testing.T) {
	resetFlags()
	setArgs(t, []string{"correctness-metrics", "--input", "/nonexistent/file.xml", "--range-type", "range", "--dry-run"})
	assert.Equal(t, 1, run())
}

func TestRun_InvalidRangeType(t *testing.T) {
	resetFlags()
	xmlPath := writeTempXML(t, sampleXML)
	setArgs(t, []string{"correctness-metrics", "--input", xmlPath, "--range-type", "foo"})
	assert.Equal(t, 1, run())
}

func TestRun_MissingRemoteWriteURL(t *testing.T) {
	resetFlags()
	xmlPath := writeTempXML(t, sampleXML)
	// Not dry-run, no --remote-write-url
	setArgs(t, []string{"correctness-metrics", "--input", xmlPath, "--range-type", "range"})
	assert.Equal(t, 1, run())
}

func TestRun_UsernameWithoutPassword(t *testing.T) {
	resetFlags()
	xmlPath := writeTempXML(t, sampleXML)
	setArgs(t, []string{"correctness-metrics", "--input", xmlPath, "--range-type", "range", "--dry-run", "--remote-write-username", "user"})
	assert.Equal(t, 1, run())
}
