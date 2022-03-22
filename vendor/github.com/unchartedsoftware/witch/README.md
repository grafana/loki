# witch

> Dead simple watching

[![Build Status](https://travis-ci.org/unchartedsoftware/witch.svg?branch=master)](https://travis-ci.org/unchartedsoftware/witch)
[![Go Report Card](https://goreportcard.com/badge/github.com/unchartedsoftware/witch)](https://goreportcard.com/report/github.com/unchartedsoftware/witch)

<img width="600" src="https://rawgit.com/unchartedsoftware/witch/master/screenshot.gif" alt="screenshot" />

## Description

Detects changes to files or directories and executes the provided shell command. That's it.

## Features:
- Supports [double-star globbing](https://github.com/bmatcuk/doublestar)
- Detects files / directories added after starting watch
- Watch will persist if executed shell command returns an error (ex. compile / linting errors)
- Awesome magic wand terminal spinner

**Note**: Uses polling to work consistently across multiple platforms, therefore the CPU usage is dependent on the efficiently of your globs. With a minimal effort your watch should use very little CPU. Will switch to event-based once [fsnofity](https://github.com/fsnotify/fsnotify) has matured sufficiently.

## Dependencies

Requires the [Go](https://golang.org/) programming language binaries with the `GOPATH` environment variable specified and `$GOPATH/bin` in your `PATH`.

## Installation

```bash
go get github.com/unchartedsoftware/witch
```

## Usage

```bash
witch --cmd=<shell-command> [--watch="<glob>,..."] [--ignore="<glob>,..."] [--interval=<milliseconds>]
```

Command-line args:

Flag             | Description
---------------- | -------
`cmd`            | Shell command to execute after changes are detected
`watch`          | Comma separated globs to watch (default: ".")
`ignore`         | Comma separated globs to ignore (default: "")
`interval`       | Scan interval in milliseconds (default: 400)
`max-token-size` | Max output token size in bytes (default: 2048000)
`no-spinner`     | Disable fancy terminal spinner (default: false)

## Globbing

Globbing rules are the same as [doublestar](https://github.com/bmatcuk/doublestar) which supports the following special terms in the patterns:

Special Terms | Meaning
------------- | -------
`*`           | matches any sequence of non-path-separators
`**`          | matches any sequence of characters, including path separators
`?`           | matches any single non-path-separator character
`[class]`     | matches any single non-path-separator character against a class of characters ([see below](#character-classes))
`{alt1,...}`  | matches a sequence of characters if one of the comma-separated alternatives matches

Any character with a special meaning can be escaped with a backslash (`\`).

### Character Classes

Character classes support the following:

Class      | Meaning
---------- | -------
`[abc]`    | matches any single character within the set
`[a-z]`    | matches any single character in the range
`[^class]` | matches any single character which does *not* match the class

Any globs resolved to a directory will be have all contents watched.

## Example

```bash
witch --cmd="make lint && make fmt && make run" --watch="main.go,api/**/*.go"
```
