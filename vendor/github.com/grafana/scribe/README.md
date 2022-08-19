# scribe

Scribe is a framework for writing flexible CI pipelines in Go that have consistent behavior when ran locally or in a CI server.

## Status

This is still in beta. Expect breaking changes and incomplete features.

## Why?

With Scribe you can:

- Run pipelines locally for testing.
- Generate configurations for existing CI tools.
- Use Go to debug your pipelines.
- Use existing Go tools to make complex pipelines easier to develop and maintain.

## Running Locally / testing

**Note**: For examples, please see the [demo](demo/) folder.

### With the `scribe` CLI

|                                             |                                              |
| ------------------------------------------- | -------------------------------------------- |
| Compile the Scribe utility                  | `mage build`                                 |
| Run the local pipeline in the current shell | `./bin/scribe ./ci`                          |
| Run the local pipeline in docker            | `./bin/scribe -mode=docker ./ci`             |
| Generate the drone                          | `./bin/scribe -mode=drone ./ci`              |
| Generate the drone and write it to a file   | `./bin/scribe -mode=drone ./ci > .drone.yml` |

### Without the `scribe` CLI

|                                             |                                        |
| ------------------------------------------- | -------------------------------------- |
| Run the local pipeline in the current shell | `go run ./ci`                          |
| Run the local pipeline in docker            | `go run ./ci -mode=docker`             |
| Generate the drone                          | `go run ./ci -mode=drone`              |
| Generate the drone and write it to a file   | `go run ./ci -mode=drone > .drone.yml` |

## How?

`scribe` does not create pipelines using templating. It uses pipeline definitions as a compilation target. Rather than templating a YAML file, `scribe` will create one that best represents the pipeline you've defined.

## Tips for writing a Pipeline

1. Every pipeline is a program and must have a `package main` and a `func main`.
2. Every pipeline must have a form of `pipeline := scribe.New(...)` or `pipeline := scribe.NewMulti(...)` to produce the scribe object.

   - Steps are then added to that object to create a pipeline.

3. Every pipeline must conclude with `pipeline.Done()`

4. It is recommended to create a Go workspace for your CI pipeline with `go work init {directory}`.

   - This will keep the larger and irrelevant modules like `docker` out of your project.

### Examples

To view examples of pipelines, visit the [demo](./demo) folder. These demos are used in our automated tests.

## FAQ

- **Why use Go and not `JavaScript/TypeScript/Python/Java`?**

We use Go pretty ubiquitously at Grafana, especially in our server code. Go also allows you to easily compile a static binary for Linux from any platform which helps a lot with the portability of Scribe, especially in Docker mode.

- **Will there be support for any other languages?**

Given the current design, it would be very difficult and there are no concrete plans to do that yet.

- **What clients are available?**

- `cli`, which runs the pipeline in the current shell. This mode is easy to use and fast, however it may not be indicative of how the pipeline will run in a remote context.
- `docker`, which runs the pipeline using the docker daemon (configured via the Docker environment variables). When developing a pipeline, this is the recommended way to run it.
- `drone`, which produces a .drone.yml file in the standard output stream (`stdout`) that will run the pipeline in Drone.
- `drone-starlark`, which produces a `.star` / starlark-compatible file in the standard output stream (`stdout`) for use in Drone pipelines.

The current list of clients can always be obtained using the `scribe -help` command.

- **How can I use unsupported clients or make my own?**

Because Scribe is simply a package and your pipeline is a program, you can add a client you have made yourself in your pipeline.

In the `init` function of the pipeline, simply register your client and it should be available for use. For a demonstration, see [`./demo/custom-client`](./demo/custom-client).

- **What features are currently available and what's planned for the future?**

Take a look at the issues and milestones to get an idea. The [demo](./demo) folder is a good place to see what's currently available for you to use.
