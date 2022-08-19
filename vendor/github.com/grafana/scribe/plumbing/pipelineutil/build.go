package pipelineutil

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	golangx "github.com/grafana/scribe/golang/x"
)

// GoBuildOpts is the list of (mostly) optional arguments that can be provided when building a pipeline into a static binary.
// The goal of compiling the pipeline into a binary is that it will be mounted into a container and used in that container.
type GoBuildOpts struct {
	// Pipeline is the path to the pipeline that you want to compile.
	// This path should be a `go build` compatible path.
	Pipeline string
	// Module is the path to the root module of the project that defines the go.mod/go.sum for the pipeline.
	// If this value is not provided, then 'GoBuild' will assume this is the value of 'os.Getwd'.
	Module string
	// GoOS sets the "GOOS" environment variable.
	// if not set, will not be supplied to the command, defaulting it to the current OS.
	GoOS string // GoArch sets the "GOARCH" environment variable.
	// If not set, will not be supplied to the command, defaulting it to the current architecture.
	GoArch string
	// GoModCache sets the "GOMODCACHE" environment variable.
	// 'go build' requires a location to search for the go module cache.
	// if this is not set, then it uses the value available from the current environment using 'os.Getenv'.
	GoModCache string
	// GoPath sets the "GOPATH" environment variable.
	// 'go build' requires a $GOPATH to be set.
	// if this is not set, then it uses the value available from the current environment using 'os.Getenv'.
	GoPath string
	// Output is used as the '-o' argument in the go build command.
	// If this is not set, then we do not provide it, causing the compiled pipeline to be built in the 'os.Getwd', with a potentially confusing or ambiguous (or even colliding) name.
	Output string
	Stdout io.Writer
	Stderr io.Writer
}

func goBuildEnv(opts GoBuildOpts) []string {
	env := []string{
		"CGO_ENABLED=0",
	}
	var (
		goOS       = opts.GoOS
		goArch     = opts.GoArch
		goModCache = opts.GoModCache
	)

	if goOS != "" {
		env = append(env, fmt.Sprintf("GOOS=%s", goOS))
	}
	if goArch != "" {
		env = append(env, fmt.Sprintf("GOARCH=%s", goArch))
	}

	if goModCache == "" {
		goModCache = os.Getenv("GOMODCACHE")
	}

	env = append(env, fmt.Sprintf("GOMODCACHE=%s", goModCache))
	return env
}

// GoBuild returns the *exec.Cmd will, if ran, statically compile the pipeline provided in the arguments.
// This function shells out to the 'go' process, so ensure that 'go' is installed and available in the current environment.
// We have to shell out because Go does not provide a stdlib function for running 'go build' without the 'go' command.
func GoBuild(ctx context.Context, opts GoBuildOpts) *exec.Cmd {
	var (
		wd  = filepath.Clean(opts.Module)
		env = goBuildEnv(opts)
	)

	return golangx.Build(ctx, golangx.BuildOpts{
		Pkg:    opts.Pipeline,
		Module: wd,
		Env:    env,
		Output: opts.Output,
	})
}
