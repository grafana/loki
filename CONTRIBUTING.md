# Contributing to Loki

Thank you for your interest in contributing to Loki! This guide covers everything you need to get started, from setting up a development environment to submitting your first pull request.

## Table of contents

- [Getting help](#getting-help)
- [Before you contribute](#before-you-contribute)
- [Ways to contribute](#ways-to-contribute)
- [Development setup](#development-setup)
- [Submitting a pull request](#submitting-a-pull-request)
- [Dependency management](#dependency-management)
- [Coding standards](#coding-standards)
- [Contribute to documentation](#contribute-to-documentation)
- [Contribute to Helm chart](#contribute-to-helm-chart)

## Getting help

Before opening an issue or pull request, check whether your question or idea has already been discussed:

- **Slack**: Join the `#loki` channel on [Grafana Labs Slack](https://slack.grafana.com/)
- **Community forum**: Post in the [Grafana Loki category](https://community.grafana.com/c/grafana-loki/41) on community.grafana.com

## Before you contribute

- Read the [Code of Conduct](CODE_OF_CONDUCT.md). All contributors are expected to follow it.
- Check [open issues](https://github.com/grafana/loki/issues) and [pull requests](https://github.com/grafana/loki/pulls) to avoid duplicating work.
- For questions about project direction and governance, refer to [governance](docs/sources/community/governance.md) and [MAINTAINERS.md](MAINTAINERS.md).

## Ways to contribute

- **Bug reports**: Use the [GitHub issue tracker](https://github.com/grafana/loki/issues/new/choose) and fill in the appropriate template.
- **Feature requests**: For small improvements, open an issue. For significant changes, create a [Loki Improvement Document (LID)](#loki-improvement-documents-lids) first.
- **Code changes**: Refer to [Development setup](#development-setup) and [Submitting a pull request](#submitting-a-pull-request).
- **Documentation**: Refer to [Contribute to documentation](#contribute-to-documentation).
- **Helm chart**: Refer to [Contribute to Helm chart](#contribute-to-helm-chart).

## Development setup

### Prerequisites

- **Go** 1.25 or later (check `go.mod` for the exact minimum version in use)
- **Docker** or **Podman** (for integration tests, docs preview, as well as `make` targets that run in containers)
- **Git** for version control
- **Make** for building binaries, running tests, linter, etc.

### Clone and build

```bash
git clone https://github.com/grafana/loki.git
cd loki
```

The preferred way to build is with `make`:

| Command | Output |
|---|---|
| `make loki` | `./cmd/loki/loki` |
| `make logcli` | `./cmd/logcli/logcli` |
| `make loki-canary` | `./cmd/loki-canary/loki-canary` |
| `make all` | all of the above |
| `make loki-image` | Docker image for Loki |

### Running tests

```bash
make test              # unit tests
make test-integration  # integration tests (requires Docker, takes ~15 min)
make lint              # run linters (golangci-lint)
```

### Working with local dependency overrides

Use [Go workspaces](https://go.dev/ref/mod#workspaces) to use a locally modified version of a dependency without touching `go.mod`:

```bash
go work init
go work use -r .   # recursively add sub-modules
```

The `go.work` file is gitignored and does not affect other contributors.

## Submitting a pull request

### Loki Improvement Documents (LIDs)

Before opening a large pull request to add or significantly change functionality, create a _Loki Improvement Document (LID)_. LIDs allow the community to discuss and vet ideas in an open, transparent way, inspired by Python's [PEP](https://peps.python.org/pep-0001/) and Kafka's [KIP](https://cwiki.apache.org/confluence/display/KAFKA/Kafka+Improvement+Proposals) processes.

Create a LID as a pull request using the [LID template](docs/sources/community/lids/template.md).

### Commit messages

Loki uses [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/). Every commit message must follow the format `<type>(<scope>): description`, where `(<scope>)` is optional, for example:

```
fix: Correct chunk iterator off-by-one error
feat(querier): Add partition-aware query path
feat!: Remove deprecated querier code
docs: Update upgrade guide for 3.x
```

When there is a new release, the [`CHANGELOG.md`](CHANGELOG.md) is automatically populated by [release-please](https://github.com/googleapis/release-please).
Commit messages of the same type are grouped into sections, such as `### Features` for `fix` and `### Bug Fixes` for `fix`.

The types `chore`, `docs`, `test`, `style`, `ci`, `build`, and `refactor` are not included in the changelog.
`feat!`, `fix!`, or any type with `BREAKING CHANGE:` in the footer is listed in the `### ⚠ BREAKING CHANGES` section.

Because the PR title is used directly as the changelog entry, write it as a clear, self-contained statement for a reader who has no other context.
The existing [PR checklist](#pr-checklist) item 1 gives guidance on phrasing.

### PR checklist

Before marking a PR as ready for review:

1. **Title** follows conventional commits format, uses sentence case, and starts with an imperative verb. The title appears in the changelog — write it for a reader without context (for example, `Fix latency spike when querying across multiple ingesters`).
2. **Description** clearly explains what the change does and why.
3. **Branch** is synced with `main`.
4. **Tests** are added or updated where appropriate.
5. **Upgrade guide** at `docs/sources/setup/upgrade/_index.md` is updated if the change affects any of:
   - Any breaking changes
   - Default configuration values
   - Metric or label names
   - Log lines used in dashboards or alerts (e.g., lines in `metrics.go` files)
   - Configuration parameters
   - HTTP or gRPC API endpoints
   - Any other change requiring operator attention during an upgrade
6. **Deprecated/deleted config**: If a configuration option is deprecated or removed, update `tools/deprecated-config-checker/deprecated-config.yaml` or `deleted-config.yaml` respectively ([example PR](https://github.com/grafana/loki/pull/10840/commits/0d4416a4b03739583349934b96f272fb4f685d15)).
7. **Documentation** is added or updated for any user-visible change, and follows the [Grafana Writers' Toolkit](https://grafana.com/docs/writers-toolkit/write/).

**Note:** A maintainer must approve and trigger CI for community contributions.

**Note:** For automated agent PRs, append 🤖🤖🤖 to the PR title to opt into a dedicated agent review process.

## Dependency management

Loki uses [Go modules](https://golang.org/cmd/go/#hdr-Modules__module_versions__and_more) for dependency management.

To add or update a dependency:

```bash
# Pick the latest tagged release
go get example.com/some/module/pkg

# Pin a specific version
go get example.com/some/module/pkg@vX.Y.Z

# Tidy and vendor
go mod tidy
go mod vendor
git add go.mod go.sum vendor
git commit
```

Always commit changes to `go.mod`, `go.sum`, and `vendor/` together in the same commit.

## Coding standards

Refer to [CODING_STANDARDS.md](CODING_STANDARDS.md) for the full style guide. Standards are enforced by CI and can be validated with the `make lint` command before submitting a PR.

## Contribute to documentation

We're glad you're here to help make our technical documentation even better for Loki users.

Loki's documentation lives in `docs/sources/` and is published to [grafana.com/docs/loki/latest](https://grafana.com/docs/loki/latest/).

The Grafana docs team has created a [Writers' Toolkit](https://grafana.com/docs/writers-toolkit/) that includes information about how we write docs, a [Style Guide](https://grafana.com/docs/writers-toolkit/write/style-guide/), and templates to help you contribute to the Loki documentation.

The Loki documentation is written using the CommonMark flavor of markdown, including some extended features. For more information about markdown, you can see the [CommonMark specification](https://spec.commonmark.org/), and a [quick reference guide](https://commonmark.org/help/) for CommonMark.

Loki uses the static site generator [Hugo](https://gohugo.io/) to generate the documentation. Loki uses a continuous integration (CI) action to sync documentation to the Grafana. The CI is triggered on every merge to `main` in the `docs` subfolder.

If your changes need to be immediately published to the latest release, you must add the `type/docs` and the appropriate backport labels to your PR, for example, and `backport-release-2.9.x`.
However, only PRs from within the repository can be automatically backported. If this is the case, the backport label will trigger GrafanaBot to create the backport PR. Otherwise, PRs submitted via a fork require manual backporting of the changes.

* [Latest release](https://grafana.com/docs/loki/latest/)
* [Upcoming release](https://grafana.com/docs/loki/next/), at the tip of the `main` branch

### Preview docs locally

You can preview the documentation locally after installing [Docker](https://www.docker.com/) or [Podman](https://podman.io/) and building the static pages.

```bash
# Navigate to the docs/ folder with the Makefile
cd docs
# Run the make target
# This uses the grafana/docs image to generate the site
make docs
# Open http://localhost:3002/docs/loki/latest/ to review your changes
```

> If running `make docs` command gave you the following error.
> - `path /tmp/make-docs.Dcq is not shared from the host and is not known to Docker.`
> - `You can configure shared paths from Docker -> Preferences... -> Resources -> File Sharing.`

Then you can go to Docker Desktop settings and open the resources, add the temporary directory path `/tmp`.

> Note that `make docs` uses a lot of memory. If it crashes, increase the memory allocated to Docker and try again.

## Contribute to Helm chart

The Helm chart for Loki is developed and maintained in the
[grafana/helm-charts](https://github.com/grafana/helm-charts) respository.
Refer to their [CONTRIBUTING.md](https://github.com/grafana/helm-charts/blob/main/CONTRIBUTING.md) for chart specific guidelines.
