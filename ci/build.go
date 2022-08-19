package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/grafana/scribe"
	"github.com/grafana/scribe/docker"
	"github.com/grafana/scribe/golang"
	"github.com/grafana/scribe/plumbing/pipeline"
)

var (
	ArgumentDatetime       = pipeline.NewStringArgument("build_time")
	ArgumentImageTag       = pipeline.NewStringArgument("image_tag")
	ArgumentGitBranch      = pipeline.NewStringArgument("git_branch")
	ArgumentGitRevision    = pipeline.NewStringArgument("git_revision")
	ArgumentGitUser        = pipeline.NewStringArgument("build_user")
	ArgumentBuildLokiImage = pipeline.NewBoolArgument("build_loki_image")
	ArgumentLokiImageTag   = pipeline.NewStringArgument("loki_image_tag")

	// cli args
	buildLokiImage = flag.Bool("build-loki-image", true, "set to false if you want to skip building the loki docker image")

	// static vars
	vPrefix                  = "github.com/grafana/loki/pkg/util/build"
	loki_base_extldflags     = "-extldflags \"-static\" -s -w"
	promtail_base_extldflags = "-extldflags -s -w"
)

func StepGetDatetime() pipeline.Step {
	action := func(ctx context.Context, opts pipeline.ActionOpts) error {
		out, err := exec.Command("date", "-u", "+\"%Y-%m-%dT%H:%M:%SZ\"").Output()
		if err != nil {
			log.Fatal(err)
		}
		r := strings.TrimSpace(string(out))
		return opts.State.SetString(ArgumentDatetime, r)
	}

	step := pipeline.NewStep(action)
	return step.Provides(ArgumentDatetime)
}

func StepGetImageTag() pipeline.Step {
	action := func(ctx context.Context, opts pipeline.ActionOpts) error {
		path := filepath.Join(opts.State.MustGetDirectoryString(pipeline.ArgumentSourceFS), "/tools/image-tag")

		out, err := exec.Command(path).Output()
		if err != nil {
			log.Fatal(err)
		}
		r := strings.TrimSpace(string(out))
		return opts.State.SetString(ArgumentImageTag, r)
	}

	step := pipeline.NewStep(action).WithArguments(pipeline.ArgumentSourceFS)
	return step.Provides(ArgumentImageTag)
}

func StepGetGitRevision() pipeline.Step {
	action := func(ctx context.Context, opts pipeline.ActionOpts) error {
		out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
		if err != nil {
			log.Fatal(err)
		}
		r := strings.TrimSpace(string(out))
		return opts.State.SetString(ArgumentGitRevision, r)
	}

	step := pipeline.NewStep(action)
	return step.Provides(ArgumentGitRevision)
}

func StepGetGitBranch() pipeline.Step {
	action := func(ctx context.Context, opts pipeline.ActionOpts) error {
		out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
		if err != nil {
			log.Fatal(err)
		}
		r := strings.TrimSpace(string(out))
		return opts.State.SetString(ArgumentGitBranch, r)
	}

	step := pipeline.NewStep(action)
	return step.Provides(ArgumentGitBranch)
}

func StepGetUser() pipeline.Step {
	action := func(ctx context.Context, opts pipeline.ActionOpts) error {
		who, err := exec.Command("whoami").Output()
		if err != nil {
			log.Fatal(err)
		}
		r := string(who)

		host, hostErr := exec.Command("hostname").Output()
		// hostname binary does not exist by default on arch
		altHost, altHostErr := exec.Command("cat", "/etc/hostname").Output()
		fmt.Println("altHost: ", altHost)
		if hostErr != nil && altHostErr != nil {
			log.Fatal(hostErr, altHostErr)
		} else if hostErr == nil {
			r = r + "@" + strings.TrimSpace(string(host))
		} else {
			r = fmt.Sprintf("%s@%s", strings.TrimSpace(r), strings.TrimSpace(string(altHost)))
		}
		opts.Logger.Infoln("host: ", r)
		return opts.State.SetString(ArgumentGitUser, r)
	}

	step := pipeline.NewStep(action)
	return step.Provides(ArgumentGitUser)
}

func StepBuildLoki() pipeline.Step {
	action := func(ctx context.Context, opts pipeline.ActionOpts) error {
		datetime := opts.State.MustGetString(ArgumentDatetime)
		image := opts.State.MustGetString(ArgumentImageTag)
		branch := opts.State.MustGetString(ArgumentGitBranch)
		version := opts.State.MustGetString(ArgumentGitRevision)
		user := opts.State.MustGetString(ArgumentGitUser)

		opts.Logger.Infoln("branch: ", branch)
		return golang.BuildAction("./cmd/loki/", "loki",
			[]string{
				"-ldflags",
				fmt.Sprintf("%s -X %s.Branch=%s -X %s.Version=%s, -X %s.BuildUser=%s -X %s.BuildDate=%s -X %s.Revision=%s", loki_base_extldflags, vPrefix, branch, vPrefix, image, vPrefix, user, vPrefix, datetime, vPrefix, version),
			}, []string{"CGO_ENABLED=0"})(ctx, opts)
	}
	step := pipeline.NewStep(action)
	return step
}

func StepSetupLokiImageTag() pipeline.Step {
	action := func(ctx context.Context, opts pipeline.ActionOpts) error {
		version := opts.State.MustGetString(ArgumentImageTag)
		return opts.State.SetString(ArgumentLokiImageTag, fmt.Sprintf("grafana/loki:%s", version))
	}
	return pipeline.NewStep(action)
}

func StepBuildImage(imagePath string) pipeline.Step {
	i := docker.Image{Dockerfile: "cmd/loki/Dockerfile", Context: "."}
	return docker.BuildImage(i, ArgumentLokiImageTag)
}

func StepBuildPromtail() pipeline.Step {
	action := func(ctx context.Context, opts pipeline.ActionOpts) error {
		datetime := opts.State.MustGetString(ArgumentDatetime)
		image := opts.State.MustGetString(ArgumentImageTag)
		branch := opts.State.MustGetString(ArgumentGitBranch)
		version := opts.State.MustGetString(ArgumentGitRevision)
		user := opts.State.MustGetString(ArgumentGitUser)

		opts.Logger.Infoln("branch: ", branch)
		return golang.BuildAction("./clients/cmd/promtail/", "promtail",
			[]string{
				"-ldflags",
				fmt.Sprintf("%s -X %s.Branch=%s -X %s.Version=%s, -X %s.BuildUser=%s -X %s.BuildDate=%s -X %s.Revision=%s", promtail_base_extldflags, vPrefix, branch, vPrefix, image, vPrefix, user, vPrefix, datetime, vPrefix, version),
				"-tags",
				"netgo",
			}, []string{"CGO_ENABLED=1"})(ctx, opts)
	}
	step := pipeline.NewStep(action)
	return step
}

// skips the step if the argument is not the expected value
func wrapBoolArgStep(step pipeline.Step, argument pipeline.Argument, expectedValue bool) pipeline.Step {
	a := step.Action
	action := func(ctx context.Context, opts pipeline.ActionOpts) error {
		arg := opts.State.MustGetBool(argument)
		if arg != expectedValue {
			opts.Logger.Infoln("skipping step")
			return nil
		}
		return a(ctx, opts)
	}
	step.Action = action
	return pipeline.NewStep(action).WithArguments(argument)
}

func lokiPipeline(sw *scribe.Scribe) {
	sw.Run(
		// setup
		StepGetDatetime().WithName("get datetime"),
		StepGetImageTag().WithName("get image tag"),
		StepGetGitBranch().WithName("get git branch"),
		StepGetGitRevision().WithName("get git revision"),
		StepGetUser().WithName("get build user info"),
		// builds
		StepBuildLoki().WithName("build loki"),
	)
}

func promtailPipeline(sw *scribe.Scribe) {
	sw.Run(
		// setup
		StepGetDatetime().WithName("get datetime"),
		StepGetImageTag().WithName("get image tag"),
		StepGetGitBranch().WithName("get git branch"),
		StepGetGitRevision().WithName("get git revision"),
		StepGetUser().WithName("get build user info"),
		// builds
		StepBuildPromtail().WithName("build promtail"),
	)
}

func lokiImagePipeline(sw *scribe.Scribe) {
	sw.Run(
		wrapBoolArgStep(StepGetImageTag().WithName("get image tag"), ArgumentBuildLokiImage, true),
		wrapBoolArgStep(StepSetupLokiImageTag().WithName("set loki image tag"), ArgumentBuildLokiImage, true),
		wrapBoolArgStep(StepBuildImage("").WithName("build loki image"), ArgumentBuildLokiImage, true),
	)
}

// "main" defines our program pipeline.
// Every pipeline step should be instantiated using the scribe client (sw).
// This allows the various client modes to work properly in different scenarios, like in a CI environment or locally.
// Logic and processing done outside of the `sw.*` family of functions may not be included in the resulting pipeline.
func main() {
	sw := scribe.NewMulti()
	defer sw.Done()

	sw.Run(
		sw.New("build-loki", lokiPipeline),
		sw.New("build-promtail", promtailPipeline),
		sw.New("build-loki-image", lokiImagePipeline),
	)
}
