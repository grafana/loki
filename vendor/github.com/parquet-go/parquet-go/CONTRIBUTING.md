# Contributing to segmentio/parquet

## Code of Conduct

Help us keep the project open and inclusive. Please be kind to and
considerate of other developers, as we all have the same goal: make
the project as good as it can be.

* [Code of Conduct](./CODE_OF_CONDUCT.md)

## Licensing

All third party contributors acknowledge that any contributions they provide
will be made under the same open source license that the open source project
is provided under.

## Contributing

* Open an Issue to report bugs or discuss non-trivial changes.
* Open a Pull Request to submit a code change for review.

### Guidelines for code review

It's good to do code review but we are busy and it's bad to wait for consensus
or opinions that might not arrive. Here are some guidelines

#### Changes where code review is optional

- Documentation changes
- Bug fixes where a reproducible test case exists
- Keeping the lights on style work (compat with new Parquet versions, new Go
  versions, for example)
- Updates to the CI environment
- Adding additional benchmarks or test cases
- Pull requests that have been open for 30 days, where an attempt has been made
  to contact another code owner, and no one has expressly requested changes

#### Changes that should get at least one code review from an owner

- Changes that may affect the performance of core library functionality
  (serializing, deserializing Parquet data) by more than 2%
- Behavior changes
- New API or changes to existing API

### Coding Rules

To ensure consistency throughout the source code, keep these rules in mind
when submitting contributions:

* All features or bug fixes must be tested by one or more tests.
* All exported types, functions, and symbols must be documented.
* All code must be formatted with `go fmt`.
