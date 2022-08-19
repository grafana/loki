package docker

import (
	"context"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/grafana/scribe/plumbing"
	"github.com/grafana/scribe/plumbing/pipeline"
)

var (
	ArgumentVersion  = pipeline.NewStringArgument("version")
	ArgumentImageTag = pipeline.NewStringArgument("tag")
)

type ImageData struct {
	Version string
}

type Image struct {
	Name       string
	Dockerfile string
	Context    string
}

func (i Image) Tag(version string) string {
	// hack: if the image doesn't have a name then it must be the default one!
	name := plumbing.DefaultImage(version)

	if i.Name != "" {
		name = plumbing.SubImage(i.Name, version)
	}

	return name
}

func Client() *docker.Client {
	client, err := docker.NewClientFromEnv()
	if err != nil {
		panic(err)
	}

	return client
}

func BuildImage(image Image, arg pipeline.Argument) pipeline.Step {
	action := func(ctx context.Context, opts pipeline.ActionOpts) error {
		client := Client()
		version := opts.State.MustGetString(arg)

		opts.Logger.Infoln("Building", image.Dockerfile, "with tag", version)

		return client.BuildImage(docker.BuildImageOptions{
			Context:    ctx,
			Name:       version,
			Dockerfile: image.Dockerfile,
			ContextDir: image.Context,
			BuildArgs: []docker.BuildArg{
				{
					Name:  "VERSION",
					Value: version,
				},
			},
			Labels: map[string]string{
				"source": "scribe",
			},
			OutputStream: opts.Stdout,
		})
	}

	return pipeline.NewStep(action).
		WithArguments(pipeline.ArgumentSourceFS, pipeline.ArgumentDockerSocketFS, arg)
}
