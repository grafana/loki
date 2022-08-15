package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"os/exec"

	"github.com/grafana/scribe"
	"github.com/grafana/scribe/golang"
	"github.com/grafana/scribe/jsonnet"

	// "github.com/grafana/scribe/jsonnet"
	"github.com/grafana/scribe/plumbing/pipeline"
)

var (
	ArgumentDatetime    = pipeline.NewStringArgument("build_time")
	ArgumentImageTag    = pipeline.NewStringArgument("image_tag")
	ArgumentGitBranch   = pipeline.NewStringArgument("git_branch")
	ArgumentGitRevision = pipeline.NewStringArgument("git_revision")
	ArgumentGitUser     = pipeline.NewStringArgument("build_user")

	// static vars
	vPrefix         = "github.com/grafana/loki/pkg/util/build"
	base_extldflags = "-extldflags \"-static\" -s -w"
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
	// │ File: tools/image-tag
	// ───────┼────────────────────────────────────────────────────────────────────────────────────────────────
	//    1   │ #!/usr/bin/env bash
	//    2   │
	//    3   │ set -o errexit
	//    4   │ set -o nounset
	//    5   │ set -o pipefail
	//    6   │
	//    7   │ WIP=$(git diff --quiet || echo '-WIP')
	//    8   │ BRANCH=$(git rev-parse --abbrev-ref HEAD | sed 's#/#-#g')
	//    9   │ # When 7 chars are not enough to be unique, git automatically uses more.
	//   10   │ # We are forcing to 7 here, as we are doing for grafana/grafana as well.
	//   11   │ SHA=$(git rev-parse --short=7 HEAD | head -c7)
	//   12   │
	//   13   │ # If not a tag, use branch-hash else use tag
	//   14   │ TAG=$((git describe --exact-match 2> /dev/null || echo "") | sed 's/v//g')
	//   15   │
	//   16   │ if [ -z "$TAG" ]
	//   17   │ then
	//   18   │       echo "${BRANCH}"-"${SHA}""${WIP}"
	//   19   │ else
	//   20   │       echo "${TAG}"
	//   21   │ fi

	action := func(ctx context.Context, opts pipeline.ActionOpts) error {
		out, err := exec.Command("../tools/image-tag").Output()
		if err != nil {
			log.Fatal(err)
		}
		r := strings.TrimSpace(string(out))
		return opts.State.SetString(ArgumentImageTag, r)
	}

	step := pipeline.NewStep(action)
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
			// r = r + "@" + strings.TrimSpace(string(altHost)) //string(altHost)
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
		return golang.BuildAction("../cmd/loki/", "loki",
			[]string{
				"-ldflags",
				fmt.Sprintf("%s -X %s.Branch=%s -X %s.Version=%s, -X %s.BuildUser=%s -X %s.BuildDate=%s -X %s.Revision=%s", base_extldflags, vPrefix, branch, vPrefix, image, vPrefix, user, vPrefix, datetime, vPrefix, version),
			}, []string{"CGO_ENABLED=0"})(ctx, opts)
	}
	step := pipeline.NewStep(action)
	return step
}

// func StepLintJsonnet() pipeline.Step {

// }

// "main" defines our program pipeline.
// Every pipeline step should be instantiated using the scribe client (sw).
// This allows the various client modes to work properly in different scenarios, like in a CI environment or locally.
// Logic and processing done outside of the `sw.*` family of functions may not be included in the resulting pipeline.
func main() {
	sw := scribe.New("basic pipeline")
	defer sw.Done()

	// sw.When(
	// 	pipeline.GitCommitEvent(pipeline.GitCommitFilters{
	// 		Branch: pipeline.StringFilter("main"),
	// 	}),
	// 	pipeline.GitTagEvent(pipeline.GitTagFilters{
	// 		Name: pipeline.GlobFilter("v*"),
	// 	}),
	// )

	// In parallel, install the yarn and go dependencies, and cache the node_modules and $GOPATH/pkg folders.
	// The cache should invalidate if the yarn.lock or go.sum files have changed
	// sw.Run(
	// 	pipeline.NamedStep("install frontend dependencies", sw.Cache(
	// 		yarn.InstallAction(),
	// 		fs.Cache("node_modules", fs.FileHasChanged("yarn.lock")),
	// 	)),
	// 	pipeline.NamedStep("install backend dependencies", sw.Cache(
	// 		golang.ModDownload(),
	// 		fs.Cache("$GOPATH/pkg", fs.FileHasChanged("go.sum")),
	// 	)),
	// 	writeVersion(sw).WithName("write-version-file"),
	// )

	// sw.Opts.State.SetString(ArgumentVPrefix, "github.com/grafana/loki/pkg/util/build")

	sw.Run(
		StepGetDatetime().WithName("get datetime"),
		StepGetImageTag().WithName("get image tag"),
		StepGetGitBranch().WithName("get git branch"),
		StepGetGitRevision().WithName("get git revision"),
		StepGetUser().WithName("get build user info"),

		StepBuildLoki().WithName("build loki"),
		// buildLoki(),
		// golang.BuildStep("../cmd/loki/", "loki", []string{"X", fmt.Sprintf("%s.Branch=%s", vPrefix, branch)}, []string{"CGO_ENABLED=0"}).WithName("compile Loki"),
		// pipeline.NamedStep("build Loki", golang.BuildStep("../.", "hello?")),
		// pipeline.NamedStep("compile Loki", makefile.Target("build")),
		// pipeline.NamedStep("compile frontend", makefile.Target("package")),
		// pipeline.NamedStep("build docker image", makefile.Target("build")).WithArguments(pipeline.ArgumentDockerSocketFS),
	)

	sw.Run(
		jsonnet.Lint("asdf"),
	)

	// sw.Run(
	// 	pipeline.NamedStep("publish", makefile.Target("publish")).
	// 		WithArguments(
	// 			pipeline.NewSecretArgument("gcs-publish-key"),
	// 		),
	// )
}
