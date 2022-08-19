package drone

import (
	"context"
	"fmt"
	"net/url"
	"path"

	"github.com/drone/drone-yaml/yaml"
	"github.com/drone/drone-yaml/yaml/pretty"
	"github.com/grafana/scribe/plumbing"
	"github.com/grafana/scribe/plumbing/pipeline"
	"github.com/grafana/scribe/plumbing/pipeline/clients/drone/starlark"
	"github.com/grafana/scribe/plumbing/pipelineutil"
	"github.com/grafana/scribe/plumbing/stringutil"
	"github.com/sirupsen/logrus"
)

var (
	ErrorNoImage = plumbing.NewPipelineError("no image provided", "An image is required for all steps in Drone. You can specify one with the '.WithImage(\"name\")' function.")
	ErrorNoName  = plumbing.NewPipelineError("no name provided", "A name is required for all steps in Drone. You can specify one with the '.WithName(\"name\")' function.")
)

type Client struct {
	Opts pipeline.CommonOpts

	Log *logrus.Logger

	// Language defines the language of the pipeline that will be generated.
	// For example, if Language is LanguageYAML, then the generated pipeline
	// will be the yaml that will tell drone to run the pipeline as it is written.
	//
	// Other languages will be generated using functions that you can use within
	// your own pipelines in those other languages. They are intended to facilitate
	// transitions between other systems and Scribe.
	Language DroneLanguage
}

func (c *Client) Validate(step pipeline.Step) error {
	if step.Image == "" {
		return ErrorNoImage
	}

	if step.Name == "" {
		return ErrorNoName
	}

	return nil
}

func stepsToNames(steps []pipeline.Step) []string {
	s := make([]string, len(steps))
	for i, v := range steps {
		s[i] = stringutil.Slugify(v.Name)
	}

	return s
}

func pipelinesToNames(steps []pipeline.Pipeline) []string {
	s := make([]string, len(steps))
	for i, v := range steps {
		s[i] = stringutil.Slugify(v.Name)
	}

	return s
}

type stepList struct {
	steps    []*yaml.Container
	services []*yaml.Container
}

func (s *stepList) AddStep(step *yaml.Container) {
	s.steps = append(s.steps, step)
}

func (s *stepList) AddService(step *yaml.Container) {
	s.services = append(s.services, step)
}

func (c *Client) StepWalkFunc(log logrus.FieldLogger, s *stepList, state string) func(ctx context.Context, steps ...pipeline.Step) error {
	return func(ctx context.Context, steps ...pipeline.Step) error {
		log.Debugf("Processing '%d' steps...", len(steps))
		for _, v := range steps {
			log := log.WithField("dependencies", stepsToNames(v.Dependencies))
			log.Debugf("Processing step '%s'...", v.Name)
			step, err := NewStep(c, c.Opts.Args.Path, state, c.Opts.Version, v)
			if err != nil {
				return err
			}

			if v.IsBackground() {
				s.AddService(step)
				log.Debugf("Done Processing background step '%s'.", v.Name)
				continue
			}

			step.DependsOn = stepsToNames(v.Dependencies)
			s.AddStep(step)
			log.Debugf("Done Processing step '%s'.", v.Name)
		}
		log.Debugf("Done processing '%d' steps...", len(steps))
		return nil
	}
}

var (
	PipelinePath = "/var/scribe/pipeline"
	StatePath    = "/var/scribe-state"
	ScribeVolume = &yaml.Volume{
		Name:     "scribe",
		EmptyDir: &yaml.VolumeEmptyDir{},
	}
	HostDockerVolume = &yaml.Volume{
		Name: stringutil.Slugify(pipeline.ArgumentDockerSocketFS.Key),
		HostPath: &yaml.VolumeHostPath{
			Path: "/var/run/docker.sock",
		},
	}
	ScribeStateVolume = &yaml.Volume{
		Name:     "scribe-state",
		EmptyDir: &yaml.VolumeEmptyDir{},
	}
	ScribeVolumeMount = &yaml.VolumeMount{
		Name:      "scribe",
		MountPath: "/var/scribe",
	}
	ScribeStateVolumeMount = &yaml.VolumeMount{
		Name:      "scribe-state",
		MountPath: StatePath,
	}
)

type newPipelineOpts struct {
	Name      string
	Steps     []*yaml.Container
	Services  []*yaml.Container
	DependsOn []string
}

func (c *Client) newPipeline(opts newPipelineOpts, pipelineOpts pipeline.CommonOpts) *yaml.Pipeline {
	command := pipelineutil.GoBuild(context.Background(), pipelineutil.GoBuildOpts{
		Pipeline: pipelineOpts.Args.Path,
		Output:   PipelinePath,
	})

	build := &yaml.Container{
		Name:    "builtin-compile-pipeline",
		Image:   plumbing.SubImage("go", c.Opts.Version),
		Command: command.Args,
		Environment: map[string]*yaml.Variable{
			"GOOS": {
				Value: "linux",
			},
			"GOARCH": {
				Value: "amd64",
			},
			"CGO_ENABLED": {
				Value: "0",
			},
		},
		Volumes: []*yaml.VolumeMount{ScribeVolumeMount},
	}

	// Add the approprirate steps and "DependsOn" to each step.
	for i, v := range opts.Steps {
		if len(v.DependsOn) == 0 {
			opts.Steps[i].DependsOn = []string{build.Name}
		}
		opts.Steps[i].Volumes = append(opts.Steps[i].Volumes, ScribeVolumeMount, ScribeStateVolumeMount)
	}

	p := &yaml.Pipeline{
		Name:      opts.Name,
		Kind:      "pipeline",
		Type:      "docker",
		DependsOn: opts.DependsOn,
		Steps:     append([]*yaml.Container{build}, opts.Steps...),
		Services:  opts.Services,
		Volumes: []*yaml.Volume{
			ScribeVolume,
			ScribeStateVolume,
			HostDockerVolume,
		},
	}

	return p
}

// Done traverses through the tree and writes a .drone.yml file to the provided writer
func (c *Client) Done(ctx context.Context, w pipeline.Walker) error {
	cfg := []yaml.Resource{}
	log := c.Log.WithField("client", "drone")

	// StatePath is an aboslute path and already has a '/'.
	state := &url.URL{
		Scheme: "file",
		Path:   path.Join(StatePath, "state.json"),
	}

	err := w.WalkPipelines(ctx, func(ctx context.Context, pipelines ...pipeline.Pipeline) error {
		log.Debugf("Walking '%d' pipelines...", len(pipelines))
		for _, v := range pipelines {
			log.Debugf("Processing pipeline '%s'...", v.Name)
			sl := &stepList{}
			if err := w.WalkSteps(ctx, v.ID, c.StepWalkFunc(log, sl, state.String())); err != nil {
				return err
			}

			pipeline := c.newPipeline(newPipelineOpts{
				Name:      stringutil.Slugify(v.Name),
				Steps:     sl.steps,
				Services:  sl.services,
				DependsOn: pipelinesToNames(v.Dependencies),
			}, c.Opts)
			if len(v.Events) == 0 {
				log.Debugf("Pipeline '%d' / '%s' has 0 events", v.ID, v.Name)
			} else {
				log.Debugf("Pipeline '%d' / '%s' has '%d events", v.ID, v.Name, len(v.Events))
			}
			events := v.Events
			if len(events) != 0 {
				log.Debugf("Generating with %d event filters...", len(events))
				cond, err := Events(events)
				if err != nil {
					return err
				}

				pipeline.Trigger = cond
			}

			log.Debugf("Done processing pipeline '%s'", v.Name)
			cfg = append(cfg, pipeline)
		}
		return nil
	})

	if err != nil {
		return err
	}

	switch c.Language {
	case LanguageYAML:
		manifest := &yaml.Manifest{
			Resources: cfg,
		}
		pretty.Print(c.Opts.Output, manifest)

	case LanguageStarlark:
		if err := c.renderStarlark(cfg); err != nil {
			c.Log.WithError(err).Warn("error rendering starlark")
		}

	default:
		return fmt.Errorf("unknown Drone language: %d", c.Language)
	}

	return nil
}

func (c *Client) renderStarlark(cfg []yaml.Resource) error {
	sl := starlark.NewStarlark()
	for _, resource := range cfg {
		switch t := resource.(type) {
		case *yaml.Pipeline:
			sl.MarshalPipeline(t)

		default:
			return fmt.Errorf("unrecognized type: %s: resource %v", t, resource)
		}
	}

	fmt.Fprint(c.Opts.Output, sl.String())
	return nil
}
