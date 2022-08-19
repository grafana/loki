# Development guide

## Layout

This project is designed with two main elements in mind:

- The library that pipeline developers interact with, typically located in the root package of the project.
- The internal packages that do all of the real work. This is often refered to as the "plumbing"

It is important that the "pipeline-developer-facing" stays as readable and minimal as possible and doesn't contain excessive implementation details.

```
.
|─./scribe.go
├── ci
├── demo
├── {package}
│   └── x
└── plumbing
    ├── cmd
    │   └── commands
    ├── pipeline
    │   └── clients
    ├── plog
    ├── schemas
    └── {x}util
```

| directory / format            | description                                                                                                                                                                     |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `./initializers.go`           | Defines functions and logic for initializing different clients.                                                                                                                 |
| `./scribe*.go`            | Defines the `Client` interface and wrapper types that Pipeline developers use to create pipelines.                                                                              |
| `./ci`                        | Scribe pipeline that tests / builds this repository.                                                                                                                        |
| `./demo`                      | Demo pipelines. Each sub-directory should be a separate pipeline that introduces a new / separate concept.                                                                      |
| `./.compose`                  | Contains configuration files for the different services in 'docker-compose'                                                                                                     |
| `./{package}`                 | Represents a Go package that should contain only definitions for **Steps** or **Actions** for use in Scribe pipelines.                                                      |
| `./{package}/x`               | The small, unit-testable functions that power the actions used in `{package}`.                                                                                                  |
| `./plumbing`                  | The packages that power the pipeline logic including asyncronous / goroutine handling and client code.                                                                          |
| `./plumbing/cmd`              | The `main` package and commands that make up the `scribe` binary.                                                                                                           |
| `./plumbing/pipeline`         | The types that make up a Pipeline, regardless of client. Primarily `Collection`, `Step` and `Action`.                                                                           |
| `./plumbing/pipeline/clients` | The Clients that satisfy the `Client` interface. These Clients can run the pipeline in an environment of some kind, or can generate configuration that represents the pipeline. |
| `./plumbing/plog`             | The Logger that is used in Scribe Clients.                                                                                                                                  |
| `./plumbing/schemas`          | Strictly contains types that represent third-party configuration schemas that Clients will use for generation. (TODO: Maybe those schemas should live next to the clients?      |
| `./plumbing/{x}util`          | Specific utility packages that help with {x}. For example, {sync}util helps with using the {sync} package.                                                                      |

Important notes:

- Try to limit the amount of non-Step/Action logic in a `./{package}` to a minimum and delegate that logic to the `x` sub-package.
  - For example, the `Docker` client builds a Go binary of the requested pipeline, but does not perform this action in the pipeline as a Step, so it uses the `pkg.grafana.com/scribe/v1/golang/x` package.
- Packages inside `plumbing` should contain code that a pipeline developer is not actively encouraged to import.
  - This is intentionally restrictive; packages OUTSIDE plumbing should only be there if pipeline developers are encouraged to use them.

## Style guide

### Global style suggestions

- Prefer using standard library packages over third party ones.
  - `flag`, `os/exec`

## Command-line arguments

There are two places where command-line arguments are parsed:

1. In the `scribe.New` function, which is the first function called in a pipeline.
2. In the `plumbing/cmd` package for parsing options supplied in the `scribe` command.

## Testing & Running Locally

### Setting up the Grafana, Tempo, and Loki servers using Docker Compose

#### If you want the Scribe panels...

If you want the scribe panels then follow these instructions.

There are currently not any provisioned data sources or dashboards included, so this is not super useful yet and a bit hard to set up. If you don't want them, then skip to the next section.

There is a separate project for building panel visualizations for Scribe pipelines located [here](github.com/grafana/scribe-app). Before starting Grafana, this should be cloned and compiled.

It's currently a git submodule just for ease of installation.

```
git submodule init
git submodule update
```

Next, navigate to the `scribe-app` project (located at `./compose/grafana/plugins/scribe-app` and compile it:

```
cd ./compose/grafana/plugins/scribe-app && yarn && yarn dev
```

#### Start the services

All of the services are configured using configs in the `.compose` folder.

```
docker-compose up
```

Verify that they are available by navigating to:

```
http://localhost:3000
```

#### Configure

The pipeline will work without configuring it, but logs will not be available in Loki without following these steps.

```
export $(cat .compose/.env)
```

Install a utility for sending logs to Loki. In the future this will be embedded into the Scribe library:

```
go install github.com/rfratto/lokitee@main
```

#### Run a pipeline

First, compile the `scribe` binary. The binary doesn't do much but it does provide some additional useful arguments to the pipeline itself, like `version`.

```
mage build
```

Then, run the pipeline:

```
./bin/scribe -mode=cli -log-level=info ./ci
```

To also send the logs of the pipeline to Loki, direct the stderr to stdout, and pipe the output to `lokitee`.

**Note** we log to stderr so that run modes like the `drone` mode can write complete config files to stdout.

```
./bin/scribe -mode=cli -log-level=info ./ci 2>&1 | lokitee -labels '{job="scribe"}
```

Then, to verify that this has worked successfully and that your logs are in Grafana, create your Loki data source, **set the Loki address to `http://loki:3200`**, and run this query:

```
count(rate({job="scribe"} | logfmt | __error__="" | pipeline!="" [1d] )) by(pipeline, build_id, status, completed_at)
```
