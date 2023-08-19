# Contribute

Loki uses GitHub to manage reviews of pull requests:

- If you have a trivial fix or improvement, go ahead and create a pull request.
- If you plan to do something more involved, discuss your ideas on the relevant GitHub issue.
- Make sure to follow the prerequisites below before marking your PR as ready for review.

## Loki Improvement Documents (LIDs)

Before creating a large pull request to change or add functionality, please create a _Loki Improvement Document (LID)_. We use LIDs to discuss and vet ideas submitted by maintainers or the community in an open and transparent way. As of Jan 2023, we are starting with a lightweight LID process and we may add more structure, inspired by Python's [PEP](https://peps.python.org/pep-0001/) and Kafka's [KIP](https://cwiki.apache.org/confluence/display/KAFKA/Kafka+Improvement+Proposals) approaches.

LIDs must be created as a pull request using [this template](docs/sources/community/lids/template.md).

## Pull Request Prerequisites/Checklist

1. Your PR title is in the form `<Feature Area>: Your change`.
   1. It does not end the title with punctuation. It will be added in the changelog.
   1. It starts with an imperative verb. Example: Fix the latency between System A and System B.
   1. It uses Sentence case, not Title Case.
   1. It uses a complete phrase or sentence. The PR title will appear in a changelog, so help other people understand what your change will be.
   1. It has a clear description saying what it does and why. Your PR description will be present in the project' commit log, so be gentle to it.
1. Your PR is well sync'ed with main
1. Your PR is correctly documenting appropriate changes under the CHANGELOG. You should document your changes there if:
   * It adds an important feature
   * It fixes an issue present in a previous release
   * It causes a change in operation that would be useful for an operator of Loki to know
   * You can skip this step for documentation changes, build related changes and simple bug fixes or enhancements. Rationale being we are attempting to curate the CHANGELOG entries with the most relevant and important changes that end users of Loki care about.
1. Your PR documents upgrading steps under `docs/sources/setup/upgrade/_index.md` if it changes:
   * Default configuration values
   * Metric names or label names
   * Changes existing log lines that may be used in dashboard or alerts. e.g: logs lines in any `metrics.go` files might be used in building dashboards or alerts.
   * Configuration parameters
   * Any breaking changes to HTTP or gRPC API endpoints
   * Any other change that would require special attention or extra steps to upgrade

Please document clearly what changed AND what needs to be done in the upgrade guide.

## Setup

A common problem arises in local environments when you want your module to use a locally modified dependency:
How do you make Go understand you don't want to fetch upstream dependencies but use local ones?
You could modify `go.mod` and use `replace` directives, but it's highly harming *Developer Experience* as you
need to roll back your `go.mod` before committing.

Things get even worse when you host multiple modules on the same repository as Go will ignore modifications
made locally to a module B when building a dependent module A.
Below are some solutions you can use if you happen to stumble on those problems.

### Go 1.18 workspaces

```bash
$ git clone <FORK_URL>
$ go work init     # Init your go.work file
$ go work use -r . # Recursively add sub-modules in the use clause of your go.work file
```

Since Go 1.18, we are able to launch build commands in a mode called *workspace*. Conceptually,
a workspace is an **untracked** file adding `replace` directives to your `go.mod` at runtime.
By default, Go will use the *workspace* mode when a `go.work` file is present, but you can have
different *workspaces* and specify which one to use with the `GOWORK` environment variable.

For more info, take a look at the [Proposal](https://go.googlesource.com/proposal/+/master/design/45713-workspace.md)
and the [Reference](https://go.dev/ref/mod#workspaces).

#### Go 1.17 `vendor` folder

Since Go 1.17, the `vendor` folder excludes `go.mod` file from dependencies and includes version information
in `modules.txt`. The removal of `go.mod` files inside the folder means Go won't try to use the
upstream version of our dependencies.

### Prior to Go 1.18

Prior to Go 1.18, you would need to add your fork as a remote on the original **\$GOPATH**/src/github.com/grafana/loki clone, so:

```bash
$ go get github.com/grafana/loki
$ cd $GOPATH/src/github.com/grafana/loki # GOPATH is $HOME/go by default.

$ git remote add <FORK_NAME> <FORK_URL>
```

Notice: `go get` return `package github.com/grafana/loki: no Go files in /go/src/github.com/grafana/loki` is normal.

## Contribute to helm

Please follow the [Loki Helm Chart](./production/helm/loki/README.md).

## Dependency management

We use [Go modules](https://golang.org/cmd/go/#hdr-Modules__module_versions__and_more) to manage dependencies on external packages.
This requires a working Go environment with version 1.15 or greater and git installed.

To add or update a new dependency, use the `go get` command:

```bash
# Pick the latest tagged release.
go get example.com/some/module/pkg

# Pick a specific version.
go get example.com/some/module/pkg@vX.Y.Z
```

Tidy up the `go.mod` and `go.sum` files:

```bash
go mod tidy
go mod vendor
git add go.mod go.sum vendor
git commit
```

You have to commit the changes to `go.mod` and `go.sum` before submitting the pull request.

## Coding Standards

### go imports
imports should follow `std libs`, `externals libs` and `local packages` format

Example
```
import (
	"fmt"
	"math"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
)
```

## Contribute to documentation

We're glad you're here to help make our technical documentation even better for Loki users.

The Grafana docs team has created a [Writer's Toolkit](https://grafana.com/docs/writers-toolkit/) that includes information about how we write docs, a [Style Guide](https://grafana.com/docs/writers-toolkit/write/style-guide/), and templates to help you contribute to the Loki documentation. 

The Loki documentation is written using the CommonMark flavor of markdown, including some extended features. For more information about markdown, you can see the [CommonMark specification](https://spec.commonmark.org/), and a [quick reference guide](https://commonmark.org/help/) for CommonMark.

Loki uses the static site generator [Hugo](https://gohugo.io/) to generate the documentation. Loki uses a continuous integration (CI) action to sync documentation to the [Grafana website](https://grafana.com/docs/loki/latest). The CI is triggered on every merge to main in the `docs` subfolder.

You can preview the documentation locally after installing [Docker](https://www.docker.com/) or [Podman](https://podman.io/).  

To get a local preview of the documentation:
1. Run Docker (or Podman).
2. Navigate to the directory with the makefile, `/loki/docs`.
3. Run the command `make docs`. This uses the `grafana/docs` image which internally uses Hugo to generate the static site.
4. Open http://localhost:3002/docs/loki/latest/ to review your changes.

> Note that `make docs` uses a lot of memory.  If it crashes, increase the memory allocated to Docker and try again.
