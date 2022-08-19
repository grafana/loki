package pipeline

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/opentracing/opentracing-go"
	"github.com/sirupsen/logrus"
)

type (
	StepType     int
	PipelineType int

	// Action is the function signature that a step provides when it does something.
	Action func(context.Context, ActionOpts) error
	Output interface{}
)

const (
	StepTypeDefault StepType = iota
	StepTypeBackground
)

const (
	PipelineTypeDefault PipelineType = iota
	PipelineTypeSub
)

// The ActionOpts are provided to every step that is ran.
// Each step can choose to use these options.
type ActionOpts struct {
	State  *State
	Stdout io.Writer
	Stderr io.Writer
	Tracer opentracing.Tracer
	Logger logrus.FieldLogger

	// Path is the path to the pipeline, typically provided via the `-path` argument, but automatically supplied if using the scribe CLI.
	Path string
	// Version refers to the version of Scribe that was used to run the pipeline.
	// This value is set using the `-version` argument when running a pipeline, which is automatically set by the `scribe` command.
	Version string
}

// A Step stores a Action and a name for use in pipelines.
// A Step can consist of either a single action or represent a list of actions.
type Step struct {
	// ID is the unique number that represents this step.
	// This value is used when calling `scribe -step={serial} [pipeline]`
	ID int64

	// Type represents the how the step is intended to operate. 90% of the time, the default type should be a sufficient descriptor of a step.
	// However in some circumstances, clients may want to handle a step differently based on how it's defined.
	// Background steps, for example, have to have their lifecycles handled differently.
	Type StepType

	// Name is a string that represents or describes the step, essentially the identifier.
	// Not all run modes will support using the name.
	Name string

	// Image is an optional value that can be assigned to a step.
	// Typically, in docker environments (or drone with a Docker executor), it defines the docker image that is used to run the step.
	Image string

	// Action defines the action this step performs.
	Action Action

	// Dependencies define other steps that are required to run before this one.
	// As far as we're concerned, Steps can only depend on other steps of the same type.
	Dependencies []Step

	// Arguments are arguments that are must exist in order for this step to run.
	Arguments []Argument

	// Provides are arguments that this step provides for other arguments to use in their "Arguments" list.
	ProvidesArgs []Argument

	Environment StepEnv
}

func (s Step) IsBackground() bool {
	return s.Type == StepTypeBackground
}

func (s Step) After(step Step) Step {
	if s.Dependencies == nil {
		s.Dependencies = []Step{}
	}

	s.Dependencies = append(s.Dependencies, step)

	return s
}

func (s Step) WithImage(image string) Step {
	s.Image = image
	return s
}

func (s Step) WithOutput(artifact Artifact) Step {
	return s
}

func (s Step) WithInput(artifact Artifact) Step {
	return s
}

// WithEnvVar appends a new EnvVar to the Step's environment, replacing existing EnvVars with the provided key.
// If an EnvVar is provided with a type of EnvVarArgument, then the argument is also added to this step's required arguments.
func (s Step) WithEnvVar(key string, val EnvVar) Step {
	if val.Type == EnvVarArgument {
		s = s.WithArguments(val.Argument())
	}
	return s
}

// WithEnvironment replaces the entire environment for this step.
// If an EnvVar is provided with a type of EnvVarArgument, then the argument is also added to this step's required arguments.
func (s Step) WithEnvironment(env StepEnv) Step {
	for _, v := range env {
		if v.Type == EnvVarArgument {
			s = s.WithArguments(v.Argument())
		}
	}

	s.Environment = env
	return s
}

func (s Step) ResetArguments() Step {
	s.Arguments = []Argument{}
	return s
}

func (s Step) WithArguments(args ...Argument) Step {
	s.Arguments = append(s.Arguments, args...)
	return s
}

func (s Step) Provides(arg ...Argument) Step {
	s.ProvidesArgs = arg
	return s
}

func (s Step) WithName(name string) Step {
	s.Name = name
	return s
}

// NewStep creates a new step with an automatically generated name
func NewStep(action Action) Step {
	return Step{
		Action: action,
	}
}

// NamedStep creates a new step with a name provided
func NamedStep(name string, action Action) Step {
	return Step{
		Name:   name,
		Action: action,
	}
}

func (s *StepList) Names() []string {
	names := make([]string, len(s.Steps))

	for i, v := range s.Steps {
		names[i] = v.Name
	}

	return names
}

func (s *StepList) String() string {
	return fmt.Sprintf("[%s]", strings.Join(s.Names(), " | "))
}

// DefaultAction is a nil action intentionally. In some client implementations, a nil step indicates a specific behavior.
// In Drone and Docker, for example, a nil step indicates that the docker command or entrypoint should not be supplied, thus using the default command for that image.
var DefaultAction Action = nil

// NoOpStep is used to represent a step which only exists to form uncommon relationships or for testing.
// Most clients should completely ignore NoOpSteps.
var NoOpStep = Step{
	Name: "no op",
	Action: func(context.Context, ActionOpts) error {
		return nil
	},
}

// Combine combines the list of steps into one step, combining all of their required and provided arguments, as well as their actions.
// For string values that can not be combined, like Name and Image, the first step's values are chosen.
// These can be overridden with further chaining.
func Combine(step ...Step) Step {
	s := Step{
		Name:         step[0].Name,
		Image:        step[0].Image,
		Dependencies: []Step{},
		Arguments:    []Argument{},
		ProvidesArgs: []Argument{},
	}

	for _, v := range step {
		s.Dependencies = append(s.Dependencies, v.Dependencies...)
		s.Arguments = append(s.Arguments, v.Arguments...)
		s.ProvidesArgs = append(s.ProvidesArgs, v.ProvidesArgs...)
	}

	s.Action = func(ctx context.Context, opts ActionOpts) error {
		for _, v := range step {
			if err := v.Action(ctx, opts); err != nil {
				return err
			}
		}

		return nil
	}

	return s
}

func stepListEqual(a, b []Step) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}

	return true
}

func StepNames(s []Step) []string {
	v := make([]string, len(s))
	for i := range s {
		v[i] = s[i].Name
	}

	return v
}
