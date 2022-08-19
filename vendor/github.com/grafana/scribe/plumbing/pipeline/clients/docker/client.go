package docker

import (
	"context"
	"errors"
	"fmt"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/grafana/scribe/plumbing/pipeline"
	"github.com/grafana/scribe/plumbing/plog"
	"github.com/grafana/scribe/plumbing/stringutil"
	"github.com/grafana/scribe/plumbing/syncutil"
	"github.com/sirupsen/logrus"
)

// The Client is used when interacting with a scribe pipeline using the scribe CLI.
// In order to emulate what happens in a remote environment, the steps are put into a queue before being ran.
// Each step is ran in its own docker container.
type Client struct {
	Client *docker.Client
	Opts   pipeline.CommonOpts

	Log *logrus.Logger
}

func (c *Client) Validate(step pipeline.Step) error {
	if step.Image == "" {
		return errors.New("no image provided")
	}
	return nil
}

func (c *Client) networkName(id string) string {
	return fmt.Sprintf("scribe-%s-%s", stringutil.Slugify(c.Opts.Name), id)
}

type walkOpts struct {
	walker      pipeline.Walker
	network     *docker.Network
	volume      *docker.Volume
	stateVolume *docker.Volume
	log         logrus.FieldLogger
}

func (c *Client) Done(ctx context.Context, w pipeline.Walker) error {
	id := stringutil.Random(8)

	logger := c.Log.WithFields(plog.PipelineFields(c.Opts))

	logger.Infof("Creating docker network '%s'...", c.networkName(id))
	network, err := c.Client.CreateNetwork(docker.CreateNetworkOptions{
		Context: ctx,
		Name:    c.networkName(id),
	})
	if err != nil {
		return err
	}

	logger.Infoln("Created docker network", c.networkName(id))

	logger.Infoln("Compiling pipeline in docker volume...")
	// Every step needs a compiled version of the pipeline in order to know what to do
	// without requiring that every image has a copy of the scribe binary
	volume, err := c.compilePipeline(ctx, id, network)
	if err != nil {
		return fmt.Errorf("failed to compile the pipeline in docker. Error: %w", err)
	}
	logger.Infof("Successfully compiled pipeline in volume '%s'", volume.Name)

	logger.Infoln("Creating volume for state sharing...")
	stateVolume, err := c.stateVolume(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to create state volume: %w", err)
	}
	logger.Infof("State volume '%s' created successfully", stateVolume.Name)

	logger.Infoln("Running steps in docker")
	return c.Walk(ctx, walkOpts{
		walker:      w,
		network:     network,
		volume:      volume,
		stateVolume: stateVolume,
		log:         logger,
	})
}

func (c *Client) stepWalkFunc(opts walkOpts) pipeline.StepWalkFunc {
	return func(ctx context.Context, steps ...pipeline.Step) error {
		wg := syncutil.NewWaitGroup()

		for _, step := range steps {
			log := opts.log.WithFields(logrus.Fields{
				"step":    step.Name,
				"step_id": step.ID,
			})

			log.Infoln("Creating container for step. Image:", step.Image)
			log.Debugf("With state volume: %+v", opts.stateVolume)
			log.Debugf("With binary volume: %+v", opts.volume)
			log.Debugf("With network: %+v", opts.network)
			log.Debugf("With compiled mount: %+v", MountAt(opts.volume, "/var/scribe", 777))
			log.Debugf("With state mount: %+v", MountAt(opts.stateVolume, "/var/scribe-state", 666))

			container, err := CreateStepContainer(ctx, c.Opts.State, c.Client, CreateStepContainerOpts{
				Step:     step,
				Network:  opts.network,
				Binary:   "/opt/scribe/pipeline",
				Pipeline: c.Opts.Args.Path,
				BuildID:  c.Opts.Args.BuildID,
				Out:      log.Writer(),
				Volumes: []*docker.Volume{
					{
						Name: opts.volume.Name,
					},
					{
						Name: opts.stateVolume.Name,
					},
				},
				Mounts: []docker.HostMount{
					// Mount the volume containing the compiled pipeline (`pipeline`) to /opt/scribe.
					MountAt(opts.volume, "/opt/scribe", 777),
					// Mount the scribe-state in the state volume.
					MountAt(opts.stateVolume, "/var/scribe-state", 666),
				},
			})
			if err != nil {
				return fmt.Errorf("Error creating container for step: '%s', error: %w", step.Name, err)
			}

			log.Debugf("Container created for step '%s' with image '%s'", container.Name, container.Image)

			wg.Add(func(ctx context.Context) error {
				log = log.WithField("container", container.Name)
				var (
					stdout = log.WithField("stream", "stdout").Writer()
					stderr = log.WithField("stream", "stderr").Writer()
				)

				opts := RunOpts{
					Container:  container,
					HostConfig: &docker.HostConfig{},
					Stdout:     stdout,
					Stderr:     stderr,
				}

				log.Debugln("Running container...")
				return RunContainer(ctx, c.Client, opts)
			})
		}

		return wg.Wait(ctx)
	}
}

func (c *Client) runPipeline(ctx context.Context, opts walkOpts, p pipeline.Pipeline) error {
	return opts.walker.WalkSteps(ctx, p.ID, c.stepWalkFunc(opts))
}

// walkPipelines returns the walkFunc that runs all of the pipelines in docker in the appropriate order.
// TODO: Most of this code looks very similar to the syncutil.StepWaitGroup and the ContainerWaitGroup type in this package.
// There should be a way to reduce it.
func (c *Client) walkPipelines(opts walkOpts) pipeline.PipelineWalkFunc {
	return func(ctx context.Context, pipelines ...pipeline.Pipeline) error {
		var (
			wg = syncutil.NewPipelineWaitGroup()
		)

		// These pipelines run in parallel, but must all complete before continuing on to the next set.
		for i, v := range pipelines {
			log := opts.log.WithField("pipeline", v.Name)
			// If this is a sub-pipeline, then run these steps without waiting on other pipeliens to complete.
			// However, if a sub-pipeline returns an error, then we shoud(?) stop.
			// TODO: This is probably not true... if a sub-pipeline stops then users will probably want to take note of it, but let the rest of the pipeline continue.
			// and see a report of the failure towards the end of the execution.
			if v.Type == pipeline.PipelineTypeSub {
				log.Debugln("Found sub-pipeline, running in new goroutine")
				go func(ctx context.Context, p pipeline.Pipeline) {
					if err := c.runPipeline(ctx, opts, p); err != nil {
						c.Log.WithError(err).Errorln("sub-pipeline failed")
					} else {
						log.Debugln("Sub-pipeline completed without error")
					}
				}(ctx, pipelines[i])
				continue
			}
			opts := opts
			opts.log = log
			// Otherwise, add this pipeline to the set that needs to complete before moving on to the next set of pipelines.
			wg.Add(pipelines[i], opts.walker, c.stepWalkFunc(opts))
		}

		if err := wg.Wait(ctx); err != nil {
			return err
		}

		return nil
	}
}

func (c *Client) Walk(ctx context.Context, opts walkOpts) error {
	return opts.walker.WalkPipelines(ctx, c.walkPipelines(opts))
}
