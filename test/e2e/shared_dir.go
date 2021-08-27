// +build requires_docker

package e2e

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cortexproject/cortex/integration/e2e"
)

func getProjectDirectory() (string, error) {
	if dir := os.Getenv("LOGS_ENTERPRISE_CHECKOUT_DIR"); dir != "" {
		return dir, nil
	}

	// use the git path if available
	if gitPath, err := exec.LookPath("git"); err == nil {
		dir, err := exec.Command(gitPath, "rev-parse", "--show-toplevel").Output()
		if err == nil {
			return string(bytes.TrimSpace(dir)), nil
		}
	}

	if gopath := os.Getenv("GOPATH"); gopath != "" {
		return filepath.Join(gopath, "src/github.com/grafana/loki-enterprise"), nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	return filepath.Join(cwd, "..", ".."), nil
}

func writeFileToSharedDir(s *e2e.Scenario, dst string, content []byte) error {
	dst = filepath.Join(s.SharedDir(), dst)

	// Ensure the entire path of directories exist.
	if err := os.MkdirAll(filepath.Dir(dst), os.ModePerm); err != nil {
		return err
	}

	return ioutil.WriteFile(
		dst,
		content,
		os.ModePerm)
}

func copyFileToSharedDir(s *e2e.Scenario, dst string, src string) error {
	projDir, err := getProjectDirectory()
	if err != nil {
		return fmt.Errorf("unable to determine project directory: %w", err)
	}

	data, err := ioutil.ReadFile(filepath.Join(projDir, src))
	if err != nil {
		return fmt.Errorf("unable to read file '%s' to copy to shared directory, %w", src, err)
	}

	dst = filepath.Join(s.SharedDir(), dst)

	// Ensure the entire path of directories exist.
	if err := os.MkdirAll(filepath.Dir(dst), os.ModePerm); err != nil {
		return err
	}

	return ioutil.WriteFile(
		dst,
		data,
		os.ModePerm)
}
