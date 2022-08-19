package docker

import (
	"context"
	"fmt"
	"path/filepath"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/grafana/scribe/plumbing/pipeline"
	"github.com/grafana/scribe/plumbing/pipelineutil"
)

// compilePipeline creates a docker container that compiles the provided pipeline so that the compiled pipeline can be mounted in
// other containers without requiring that the container has the scribe command or go installed.
func (c *Client) compilePipeline(ctx context.Context, id string, network *docker.Network) (*docker.Volume, error) {
	log := c.Log

	volume, err := c.Client.CreateVolume(docker.CreateVolumeOptions{
		Context: ctx,
		Name:    fmt.Sprintf("scribe-%s", id),
	})

	if err != nil {
		return nil, fmt.Errorf("error creating docker volume: %w", err)
	}

	mounts, err := DefaultMounts(volume)
	if err != nil {
		return nil, err
	}

	src, err := c.Opts.State.GetDirectoryString(pipeline.ArgumentSourceFS)
	if err != nil {
		return nil, err
	}

	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return nil, err
	}

	gomod, err := c.Opts.State.GetDirectoryString(pipeline.ArgumentPipelineGoModFS)
	if err != nil {
		return nil, err
	}

	pipelineGoMod, err := filepath.Rel(src, gomod)
	if err != nil {
		return nil, err
	}

	// Compile the pipeline, mounted in /var/scribe, to `/opt/scribe/pipeline`, which is where the volume should be mounted.
	cmd := pipelineutil.GoBuild(ctx, pipelineutil.GoBuildOpts{
		Pipeline: c.Opts.Args.Path,
		Module:   pipelineGoMod,
		Output:   "/opt/scribe/pipeline",
	})

	// Mount the path provided via the '-path' argument to /var/scribe.
	mounts = append(mounts, docker.HostMount{
		Type:   "bind",
		Source: srcAbs,
		Target: "/var/scribe",
		TempfsOptions: &docker.TempfsOptions{
			Mode: 755,
		},
	})

	opts := docker.CreateContainerOptions{
		Name: fmt.Sprintf("compile-%s", volume.Name),
		Config: &docker.Config{
			Image:      "golang:1.18",
			Cmd:        cmd.Args,
			WorkingDir: "/var/scribe",
			Env: []string{
				"GOOS=linux",
				"GOARCH=amd64",
				"CGO_ENABLED=0",
			},
		},
		HostConfig: &docker.HostConfig{
			Mounts: mounts,
		},
	}

	container, err := c.Client.CreateContainer(opts)
	if err != nil {
		return nil, err
	}

	log.Warnf("Building pipeline binary '%s' in docker volume...", c.Opts.Args.Path)
	var (
		stdout = log.WithField("stream", "stdout")
		stderr = log.WithField("stream", "stderr")
	)
	// This should run a command very similar to this:
	// docker run --rm -v $TMPDIR:/var/scribe scribe/go:{version} go build -o /var/scribe/pipeline ./{pipeline}
	if err := RunContainer(ctx, c.Client, RunOpts{
		Container:  container,
		Stdout:     stdout.Writer(),
		Stderr:     stderr.Writer(),
		HostConfig: &docker.HostConfig{},
	}); err != nil {
		return nil, err
	}

	return volume, nil
}
