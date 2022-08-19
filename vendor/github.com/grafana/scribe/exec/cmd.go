package exec

import (
	"context"
	"io"
	"os"
	"os/exec"

	"github.com/grafana/scribe/plumbing/pipeline"
)

type RunOpts struct {
	Path   string
	Stdout io.Writer
	Stderr io.Writer
	Name   string
	Args   []string
	Env    []string
}

// CommandWithOpts returns the equivalent *exec.Cmd that matches the RunOpts provided (opts).
func CommandWithOpts(ctx context.Context, opts RunOpts) *exec.Cmd {
	c := exec.CommandContext(ctx, opts.Name, opts.Args...)
	c.Dir = opts.Path

	if opts.Stdout != nil {
		c.Stdout = opts.Stdout
	}

	if opts.Stderr != nil {
		c.Stderr = opts.Stderr
	}

	c.Env = append(os.Environ(), opts.Env...)

	return c
}

// RunCommandWithOpts runs the command defined by the RunOpts provided (opts).
// Be warned that the stdout and stderr are not captured by this function and are instead written to opts.Stdout/opts.Stderr.
func RunCommandWithOpts(ctx context.Context, opts RunOpts) error {
	return CommandWithOpts(ctx, opts).Run()
}

// RunCommandAt runs a given command and set of arguments at the given location
// The command's stdout and stderr are assigned the systems' stdout/stderr streams.
func RunCommandAt(ctx context.Context, stdout, stderr io.Writer, path string, name string, arg ...string) error {
	return RunCommandWithOpts(ctx, RunOpts{
		Path:   path,
		Name:   name,
		Args:   arg,
		Stderr: stderr,
		Stdout: stdout,
	})
}

// RunCommand runs a given command and set of arguments.
// The command's stdout and stderr are assigned the systems' stdout/stderr streams.
func RunCommand(ctx context.Context, stdout, stderr io.Writer, name string, arg ...string) error {
	return RunCommandAt(ctx, stdout, stderr, ".", name, arg...)
}

// RunAction returns an action that runs a given command and set of arguments.
// The command's stdout and stderr are assigned the systems' stdout/stderr streams.
func RunAction(name string, arg ...string) pipeline.Action {
	return func(ctx context.Context, opts pipeline.ActionOpts) error {
		return RunCommand(ctx, opts.Stdout, opts.Stderr, name, arg...)
	}
}

// Run returns an action that runs a given command and set of arguments.
// The command's stdout and stderr are assigned the systems' stdout/stderr streams.
func RunAt(path string, name string, arg ...string) pipeline.Action {
	return func(ctx context.Context, opts pipeline.ActionOpts) error {
		return RunCommandAt(ctx, opts.Stdout, opts.Stderr, path, name, arg...)
	}
}

func Run(ctx context.Context, opts pipeline.ActionOpts, name string, args ...string) error {
	return RunCommandWithOpts(ctx, RunOpts{
		Name:   name,
		Args:   args,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
	})
}
