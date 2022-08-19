package cmdutil

import (
	"fmt"

	"github.com/grafana/scribe/plumbing/pipeline"
)

// CommandOpts is a list of arguments that can be provided to the StepCommand function.
type CommandOpts struct {
	// Step is the pipeline step this command is being generated for. The step contains a lot of necessary information for generating a command, mostly around arguments.
	Step pipeline.Step

	// CompiledPipeline is an optional argument. If it is supplied, this value will be used as the first argument in the command instead of the scribe command.
	// This option is useful scenarios where the 'scribe' command will not be available, but the pipeline has been compiled.
	CompiledPipeline string
	// Path is an optional argument that refers to the path of the pipeline. For example, if our plan is to have this function generate `scribe ./ci`, the 'Path' would be './ci'.
	Path string
	// BuildID is an optional argument that will be supplied to the 'scribe' command as '-build-id'.
	BuildID string

	LogLevel string

	// State is an optional argument that is supplied as '-state'. It is a path to the JSON state file which allows steps to share data.
	State string
	// StateArgs pre-populate the state for a specific step. These strings can include references to environment variables using $.
	// Environment variables are left as-is and are not substituted.
	StateArgs map[string]string

	// Version sets the `-version` argument, which is normally automatically set by the scribe CLI.
	// This value is typically used to fetch a known good docker image.
	Version string
}

// StepCommand returns the command string for running a single step.
// The path argument can be omitted, which is particularly helpful if the current directory is a pipeline.
func StepCommand(opts CommandOpts) ([]string, error) {
	args := []string{}

	if opts.BuildID != "" {
		args = append(args, fmt.Sprintf("-build-id=%s", opts.BuildID))
	}

	if opts.State != "" {
		args = append(args, fmt.Sprintf("-state=%s", opts.State))
	}

	if opts.LogLevel != "" {
		args = append(args, fmt.Sprintf("-log-level=%s", opts.LogLevel))
	}

	if opts.Version != "" {
		args = append(args, fmt.Sprintf("-version=%s", opts.Version))
	}

	if len(opts.StateArgs) != 0 {
		for k, v := range opts.StateArgs {
			args = append(args, fmt.Sprintf("-arg=%s=%s", k, v))
		}
	}

	name := "scribe"

	if p := opts.CompiledPipeline; p != "" {
		name = p
	}

	cmd := append([]string{name, fmt.Sprintf("-step=%d", opts.Step.ID)}, args...)
	if opts.Path != "" {
		cmd = append(cmd, opts.Path)
	}

	return cmd, nil
}
