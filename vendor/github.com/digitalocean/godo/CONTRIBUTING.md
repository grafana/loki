# Contributing

We love contributions! You are welcome to open a pull request, but it's a good idea to
open an issue and discuss your idea with us first.

Once you are ready to open a PR, please keep the following guidelines in mind:

1. Code should be `go fmt` compliant.
1. Types, structs and funcs should be documented.
1. Tests pass.

## Getting set up

`godo` uses go modules. Just fork this repo, clone your fork and off you go!

## Running tests

When working on code in this repository, tests can be run via:

```sh
go test -mod=vendor .
```

## Versioning

Godo follows [semver](https://www.semver.org) versioning semantics.
New functionality should be accompanied by increment to the minor
version number. Any code merged to main is subject to release.

## Releasing

The repo uses GitHub workflows to publish a draft release when a new tag is
pushed. We use [semver](https://semver.org/#summary) to determine the version
number for the tag.

1. Run `make changes` to review the merged PRs since last release and decide what kind of release you are doing (bugfix, feature or breaking).
    * Review the tags on each PR and make sure they are categorized
      appropriately.

2. Run `BUMP=(bugfix|feature|breaking) make bump_version` to update the `godo`
   version.  
   `BUMP` also accepts `(patch|minor|major)`

   Command example:

   ```bash
   make BUMP=minor bump_version
   ```

3. Update the godo version in `godo.go` and add changelog generator logs in `CHANGELOG.md` file. Create a separate PR with only these changes.

4. Once the commit has been pushed, tag the commit to trigger the
   release workflow: run `make tag` to tag the latest commit and push the tag to ORIGIN.

   Notes:
   * To tag an earlier commit, run `COMMIT=${commit} make tag`.
   * To push the tag to a different remote, run `ORIGIN=${REMOTE} make tag`.

5. Once the release process completes, review the draft release for correctness and publish the release.  
   Ensure the release has been marked `Latest`.

## Go Version Support

This project follows the support [policy of Go](https://go.dev/doc/devel/release#policy)
as its support policy. The two latest major releases of Go are supported by the project.
[CI workflows](.github/workflows/ci.yml) should test against both supported versions.
[go.mod](./go.mod) should specify the oldest of the supported versions to give
downstream users of godo flexibility.
