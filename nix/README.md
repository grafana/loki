# Nix

This folder (along with the top level `flake.nix`) contains Nix configurations to enable this project to be built by `nix`. These configurations are mostly wrappers around Makefile goals. The goal of this work is to enable people who use `nix` to integrate this project more easily in their workflows. It is not the goal of this work to replace the Makefile, and any Nix configurations added should wrap Makefile goals whenever possible so binaries built with `nix` will be identical to those built without.

## What is Nix

Nix is a package manager that can aide in reproducible builds.

## Using Nix

Using `nix` with this repository is entirely optional. You are welcome to just use the `Makefile` and maintain versions of dependent binaries in your own way.

Nix ships with commands such as `nix build` which will run our build (as defined in the `Makefile`) in a shell where the `$PATH` has been configured to have the specified version of all dependent binaries required to build and test our software (ie. `go`, `golangci-lint`, `jq`, `jsonnet`, etc), and put the resulting binary in a `result` folder. It also has a command `nix develop` which will dump you into a bash shell with a similarly configured `$PATH`. 

So, for example, if the CI lint job fails, but it's passing locally, you could run the following to make sure it's not a problem with the version of the linter installed locally on you machine:

From the root of the repo:

```console
nix develop
make lint
```

To build the repo (including running tests), from the root of the repo you can run `nix build`.

## Installing Nix

Nix is supported on Linux, MacOS, and Windows (WSL2). Check [here](https://nixos.org/download.html#download-nix) for installation instructions for your specific platform.

You will also need to enable the Flakes feature to use Nix with this repo. See this [wiki](https://wiki.nixos.org/wiki/Flakes) for instructions on enabling Flakes.
