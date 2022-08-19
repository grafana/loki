package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/grafana/scribe/plumbing/pipeline"
	"github.com/grafana/scribe/plumbing/stringutil"
)

// This function effectively runs 'git remote get-url $(git remote)'
func setCurrentRemote(state *pipeline.State) error {
	remote, err := exec.Command("git", "remote").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w. output: %s", err, string(remote))
	}

	v, err := exec.Command("git", "remote", "get-url", strings.TrimSpace(string(remote))).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w. output: %s", err, string(v))
	}

	return state.SetString(pipeline.ArgumentRemoteURL, string(v))
}

// This function effectively runs 'git rev-parse HEAD'
func setCurrentCommit(state *pipeline.State) error {
	v, err := exec.Command("git", "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w. output: %s", err, string(v))
	}

	return state.SetString(pipeline.ArgumentCommitRef, string(v))
}

// This function effectively runs 'git rev-parse --abrev-ref HEAD'
func setCurrentBranch(state *pipeline.State) error {
	v, err := exec.Command("git", "rev-parse", "--abrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w. output: %s", err, string(v))
	}

	return state.SetString(pipeline.ArgumentBranch, string(v))
}

func setWorkingDir(state *pipeline.State) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	return state.SetString(pipeline.ArgumentWorkingDir, wd)
}

func setSourceFS(state *pipeline.State) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	return state.SetDirectory(pipeline.ArgumentSourceFS, wd)
}

func setBuildID(state *pipeline.State) error {
	r := stringutil.Random(8)
	return state.SetString(pipeline.ArgumentBuildID, r)
}

// KnownValues are URL values that we know how to retrieve using the command line.
var KnownValues = map[pipeline.Argument]func(*pipeline.State) error{
	pipeline.ArgumentRemoteURL:  setCurrentRemote,
	pipeline.ArgumentCommitRef:  setCurrentCommit,
	pipeline.ArgumentBranch:     setCurrentBranch,
	pipeline.ArgumentWorkingDir: setWorkingDir,
	pipeline.ArgumentSourceFS:   setSourceFS,
	pipeline.ArgumentBuildID:    setBuildID,
}
