package docker

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/grafana/scribe/plumbing/cmdutil"
	"github.com/grafana/scribe/plumbing/pipeline"
	"github.com/grafana/scribe/plumbing/stringutil"
)

type CreateStepContainerOpts struct {
	Step     pipeline.Step
	Env      []string
	Network  *docker.Network
	Volumes  []*docker.Volume
	Mounts   []docker.HostMount
	Binary   string
	Pipeline string
	BuildID  string
	Out      io.Writer
}

func CreateStepContainer(ctx context.Context, state *pipeline.State, client *docker.Client, opts CreateStepContainerOpts) (*docker.Container, error) {
	cmd, err := cmdutil.StepCommand(cmdutil.CommandOpts{
		CompiledPipeline: opts.Binary,
		Path:             opts.Pipeline,
		Step:             opts.Step,
		BuildID:          opts.BuildID,
		State:            "file:///var/scribe-state/state.json",
	})

	if err != nil {
		return nil, err
	}

	createOpts, err := applyKnownArguments(state, docker.CreateContainerOptions{
		Context: ctx,
		Name:    strings.Join([]string{"scribe", stringutil.Slugify(opts.Step.Name), stringutil.Random(8)}, "-"),
		Config: &docker.Config{
			Image: opts.Step.Image,
			Cmd:   cmd,
			Env:   append(opts.Env, "GIT_CEILING_DIRECTORIES=/var/scribe"),
		},
		HostConfig: &docker.HostConfig{
			NetworkMode: opts.Network.Name,
			Mounts:      opts.Mounts,
		},
	}, opts.Step.Arguments)

	// TODO: We should support more docker auth config options
	authConfig, _ := docker.NewAuthConfigurationsFromDockerCfg()
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to get auth settings from docker config: %w", err)
	// }

	var (
		auth      docker.AuthConfiguration
		registry  = "https://index.docker.io/v1/"
		repo, tag = docker.ParseRepositoryTag(opts.Step.Image)
	)

	// Important note here: The returned URL will have an empty "Host" if the image repo does not begin with a URL scheme.
	// We could / should mitigate this with more parsing logic.
	if u, err := url.Parse(repo); err == nil {
		if u.Host != "" {
			registry = u.Host
		}
	}

	if authConfig != nil {
		if cfg, ok := authConfig.Configs[registry]; ok {
			auth = cfg
		}
	}

	if err := client.PullImage(docker.PullImageOptions{
		Context:           ctx,
		Repository:        repo,
		Tag:               tag,
		OutputStream:      opts.Out,
		InactivityTimeout: time.Minute,
	}, auth); err != nil {
		return nil, fmt.Errorf("error pulling image: %w", err)
	}

	return client.CreateContainer(createOpts)
}

type RunOpts struct {
	Container  *docker.Container
	HostConfig *docker.HostConfig
	Stdout     io.Writer
	Stderr     io.Writer
}

func RunContainer(ctx context.Context, client *docker.Client, opts RunOpts) error {
	if err := client.StartContainerWithContext(opts.Container.ID, opts.HostConfig, ctx); err != nil {
		return err
	}

	if err := client.AttachToContainer(docker.AttachToContainerOptions{
		Container:    opts.Container.ID,
		OutputStream: opts.Stdout,
		ErrorStream:  opts.Stderr,
		Stream:       true,
		Stdout:       true,
		Stderr:       true,
		Logs:         true,
	}); err != nil {
		return err
	}

	return nil
}
