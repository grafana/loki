// Package scribe provides the primary library / client functions, types, and methods for creating Scribe pipelines.
package scribe

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/grafana/scribe/plumbing"
	"github.com/grafana/scribe/plumbing/cmdutil"
	"github.com/grafana/scribe/plumbing/pipeline"
	"github.com/grafana/scribe/plumbing/plog"
	"github.com/opentracing/opentracing-go"
	"github.com/sirupsen/logrus"

	"github.com/uber/jaeger-client-go"
	"github.com/uber/jaeger-client-go/config"
)

var ErrorCancelled = errors.New("cancelled")

const DefaultPipelineID int64 = 1

// Scribe is the client that is used in every pipeline to declare the steps that make up a pipeline.
// The Scribe type is not thread safe. Running any of the functions from this type concurrently may have unexpected results.
type Scribe struct {
	Client     pipeline.Client
	Collection *pipeline.Collection

	// Opts are the options that are provided to the pipeline from outside sources. This includes mostly command-line arguments and environment variables
	Opts    pipeline.CommonOpts
	Log     logrus.FieldLogger
	Version string

	// n tracks the ID of a step so that the "scribe -step=" argument will function independently of the client implementation
	// It ensures that the 11th step in a Drone generated pipeline is also the 11th step in a CLI pipeline
	n        *counter
	pipeline int64

	prev          []pipeline.StepList
	prevPipelines []pipeline.Pipeline
}

// Pipeline returns the current Pipeline ID used in the collection.
func (s *Scribe) Pipeline() int64 {
	return s.pipeline
}

func nameOrDefault(name string) string {
	if name != "" {
		return name
	}

	return "default"
}

// When allows users to define when this pipeline is executed, especially in the remote environment.
func (s *Scribe) When(events ...pipeline.Event) {
	if err := s.Collection.AddEvents(s.pipeline, events...); err != nil {
		s.Log.WithError(err).Fatalln("Failed to add events to graph")
	}
}

// Background allows users to define steps that run in the background. In some environments this is referred to as a "Service" or "Background service".
// In many scenarios, users would like to simply use a docker image with the default command. In order to accomplish that, simply provide a step without an action.
func (s *Scribe) Background(steps ...pipeline.Step) {
	if err := s.validateSteps(steps...); err != nil {
		s.Log.Fatalln(err)
	}
	for i := range steps {
		steps[i].Type = pipeline.StepTypeBackground
	}

	steps = s.setup(steps...)
	list := pipeline.NewStepList(s.n.Next(), steps...)

	if err := s.Collection.AddSteps(s.pipeline, list); err != nil {
		s.Log.Fatalln(err)
	}
}

// Run allows users to define steps that are ran sequentially. For example, the second step will not run until the first step has completed.
// This function blocks the pipeline execution until all of the steps provided (step) have completed sequentially.
func (s *Scribe) Run(steps ...pipeline.Step) {
	s.Log.Debugf("Adding '%d' sequential steps: %+v", len(steps), pipeline.StepNames(steps))
	steps = s.setup(steps...)

	if err := s.runSteps(steps...); err != nil {
		s.Log.Fatalln(err)
	}
}

func (s *Scribe) runSteps(steps ...pipeline.Step) error {
	if err := s.validateSteps(steps...); err != nil {
		return err
	}

	prev := s.prev

	for _, v := range steps {
		list := pipeline.NewStepList(s.n.Next(), v)
		list.Dependencies = prev

		if err := s.Collection.AddSteps(s.pipeline, list); err != nil {
			return fmt.Errorf("Run: error adding step '%d' to collection. error: %w", list.ID, err)
		}

		prev = []pipeline.StepList{list}
	}

	s.prev = prev

	return nil
}

// Parallel will run the listed steps at the same time.
// This function blocks the pipeline execution until all of the steps have completed.
func (s *Scribe) Parallel(steps ...pipeline.Step) {
	steps = s.setup(steps...)
	if err := s.parallelSteps(steps...); err != nil {
		s.Log.Fatalln(err)
	}
}

func (s *Scribe) parallelSteps(steps ...pipeline.Step) error {
	if err := s.validateSteps(steps...); err != nil {
		return err
	}

	list := pipeline.NewStepList(s.n.Next(), steps...)
	list.Dependencies = s.prev

	if err := s.Collection.AddSteps(s.pipeline, list); err != nil {
		return fmt.Errorf("error adding step '%d' to collection. error: %w", list.ID, err)
	}

	s.prev = []pipeline.StepList{list}

	return nil
}

func (s *Scribe) Cache(action pipeline.Action, c pipeline.Cacher) pipeline.Action {
	return action
}

func (s *Scribe) setup(steps ...pipeline.Step) []pipeline.Step {
	for i, step := range steps {
		// Set a default image for steps that don't provide one.
		// Most pre-made steps like `yarn`, `node`, `go` steps should provide a separate default image with those utilities installed.
		if step.Image == "" {
			image := plumbing.DefaultImage(s.Version)
			steps[i] = step.WithImage(image)
		}

		// Set a serial / unique identifier for this step so that we can reference it using the '-step' argument consistently.
		steps[i].ID = s.n.Next()
	}

	return steps
}

func formatError(step pipeline.Step, err error) error {
	name := step.Name
	if name == "" {
		name = fmt.Sprintf("unnamed-step-%d", step.ID)
	}

	return fmt.Errorf("[name: %s, id: %d] %w", name, step.ID, err)
}

func (s *Scribe) validateSteps(steps ...pipeline.Step) error {
	for _, v := range steps {
		err := s.Client.Validate(v)
		if err == nil {
			continue
		}

		if errors.Is(err, plumbing.ErrorSkipValidation) {
			s.Log.Warnln(formatError(v, err).Error())
			continue
		}

		return formatError(v, err)
	}

	return nil
}

func (s *Scribe) watchSignals() error {
	sig := cmdutil.WatchSignals()

	return fmt.Errorf("received OS signal: %s", sig.String())
}

// Execute is the equivalent of Done, but returns an error.
// Done should be preferred in Scribe pipelines as it includes sub-process handling and logging.
func (s *Scribe) Execute(ctx context.Context, collection *pipeline.Collection) error {
	if err := s.Client.Done(ctx, collection); err != nil {
		return err
	}
	return nil
}

// SubFunc should use the provided Scribe object to populate a pipeline that runs independently.
type SubFunc func(*Scribe)

// This function adds a single sub-pipeline to the collection
func (s *Scribe) subPipeline(sub *Scribe) error {
	node, err := sub.Collection.Graph.Node(DefaultPipelineID)
	if err != nil {
		return fmt.Errorf("Failed to retrieve populated subpipeline: %w", err)
	}

	p := node.Value
	p.Type = pipeline.PipelineTypeSub
	p.ID = s.n.Next()

	if err := s.Collection.AddPipelines(p); err != nil {
		return err
	}

	return nil
}

// Sub creates a sub-pipeline. The sub-pipeline is equivalent to creating a new coroutine made of multiple steps.
// This sub-pipeline will run concurrently with the rest of the pipeline at the time of definition.
// Under the hood, the Scribe client creates a new Scribe object with a clean Collection,
// then calles the SubFunc (sf) with the new Scribe object. The collection is then populated by the SubFunc, and then appended to the existing collection.
func (s *Scribe) Sub(sf SubFunc) {
	sub := s.newSub()

	s.Log.Debugf("Populating sub-pipeline in call to Sub")
	sf(sub)
	s.Log.Debugf("Populated sub-pipeline with '%d' nodes and '%d' edges", len(sub.Collection.Graph.Nodes), len(sub.Collection.Graph.Edges))

	if err := s.subPipeline(sub); err != nil {
		s.Log.WithError(err).Fatalln("failed to add sub-pipeline")
	}
}

func (s *Scribe) newSub() *Scribe {
	id := s.n.Next()
	opts := s.Opts
	opts.Name = fmt.Sprintf("sub-pipeline-%d", id)

	collection := NewDefaultCollection(opts)

	return &Scribe{
		Client:     s.Client,
		Opts:       opts,
		Log:        s.Log.WithField("sub-pipeline", opts.Name),
		Version:    s.Version,
		n:          s.n,
		Collection: collection,
		pipeline:   DefaultPipelineID,
	}
}

func (s *Scribe) Done() {
	ctx := context.Background()

	if err := execute(ctx, s.Collection, nameOrDefault(s.Opts.Name), s.Opts, s.n, s.Execute); err != nil {
		s.Log.WithError(err).Fatal("error in execution")
	}
}

func parseOpts() (pipeline.CommonOpts, error) {
	args, err := plumbing.ParseArguments(os.Args[1:])
	if err != nil {
		return pipeline.CommonOpts{}, fmt.Errorf("Error parsing arguments. Error: %w", err)
	}

	if args == nil {
		return pipeline.CommonOpts{}, fmt.Errorf("Arguments list must not be nil")
	}

	// Create standard packages based on the arguments provided.
	// This would be a good place to initialize loggers, tracers, etc
	var tracer opentracing.Tracer = &opentracing.NoopTracer{}

	logger := plog.New(args.LogLevel)
	jaegerCfg, err := config.FromEnv()
	if err == nil {
		// Here we ignore the closer because the jaegerTracer is the closer and we will just close that.
		jaegerTracer, _, err := jaegerCfg.NewTracer(config.Logger(jaeger.StdLogger))
		if err == nil {
			logger.Infoln("Initialized jaeger tracer")
			tracer = jaegerTracer
		} else {
			logger.Infoln("Could not initialize jaeger tracer; using no-op tracer; Error:", err.Error())
		}
	}

	s, err := GetState(args.State, logger, args)
	if err != nil {
		return pipeline.CommonOpts{}, err
	}

	return pipeline.CommonOpts{
		Version: args.Version,
		Output:  os.Stdout,
		Args:    args,
		Log:     logger,
		Tracer:  tracer,
		State:   s,
	}, nil
}

func newScribe(name string) *Scribe {
	opts, err := parseOpts()
	if err != nil {
		panic(fmt.Sprintf("failed to parse arguments: %s", err.Error()))
	}

	opts.Name = name
	sw := NewClient(opts, NewDefaultCollection(opts))

	// Ensure that no matter the behavior of the initializer, we still set the version on the scribe object.
	sw.Version = opts.Args.Version
	sw.pipeline = DefaultPipelineID

	return sw
}

// New creates a new Scribe client which is used to create pipeline a single pipeline with many steps.
// This function will panic if the arguments in os.Args do not match what's expected.
// This function, and the type it returns, are only ran inside of a Scribe pipeline, and so it is okay to treat this like it is the entrypoint of a command.
// Watching for signals, parsing command line arguments, and panics are all things that are OK in this function.
// New is used when creating a single pipeline. In order to create multiple pipelines, use the NewMulti function.
func New(name string) *Scribe {
	return newScribe(name)
}

// NewWithClient creates a new Scribe object with a specific client implementation.
// This function is intended to be used in very specific environments, like in tests.
func NewWithClient(opts pipeline.CommonOpts, client pipeline.Client) *Scribe {
	if opts.Args == nil {
		opts.Args = &plumbing.PipelineArgs{}
	}

	return &Scribe{
		Client:     client,
		Opts:       opts,
		Log:        opts.Log,
		Collection: NewDefaultCollection(opts),
		pipeline:   DefaultPipelineID,

		n: &counter{1},
	}
}

// NewClient creates a new Scribe client based on the commonopts (mostly the mode).
// It does not check for a non-nil "Args" field.
func NewClient(c pipeline.CommonOpts, collection *pipeline.Collection) *Scribe {
	c.Log.Infof("Initializing Scribe client with mode '%s'", c.Args.Client)
	sw := &Scribe{
		n: &counter{},
	}

	initializer, ok := ClientInitializers[c.Args.Client]
	if !ok {
		c.Log.Fatalln("Could not initialize scribe. Could not find initializer for mode", c.Args.Client)
		return nil
	}
	sw.Client = initializer(c)
	sw.Collection = collection

	sw.Opts = c
	sw.Log = c.Log

	return sw
}
