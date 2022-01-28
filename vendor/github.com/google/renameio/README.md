[![Build Status](https://travis-ci.org/google/renameio.svg?branch=master)](https://travis-ci.org/google/renameio)
[![GoDoc](https://godoc.org/github.com/google/renameio?status.svg)](https://godoc.org/github.com/google/renameio)
[![Go Report Card](https://goreportcard.com/badge/github.com/google/renameio)](https://goreportcard.com/report/github.com/google/renameio)

The `renameio` Go package provides a way to atomically create or replace a file or
symbolic link.

## Atomicity vs durability

`renameio` concerns itself *only* with atomicity, i.e. making sure applications
never see unexpected file content (a half-written file, or a 0-byte file).

As a practical example, consider https://manpages.debian.org/: if there is a
power outage while the site is updating, we are okay with losing the manpages
which were being rendered at the time of the power outage. They will be added in
a later run of the software. We are not okay with having a manpage replaced by a
0-byte file under any circumstances, though.

## Advantages of this package

There are other packages for atomically replacing files, and sometimes ad-hoc
implementations can be found in programs.

A naive approach to the problem is to create a temporary file followed by a call
to `os.Rename()`. However, there are a number of subtleties which make the
correct sequence of operations hard to identify:

* The temporary file should be removed when an error occurs, but a remove must
  not be attempted if the rename succeeded, as a new file might have been
  created with the same name. This renders a throwaway `defer
  os.Remove(t.Name())` insufficient; state must be kept.

* The temporary file must be created on the same file system (same mount point)
  for the rename to work, but the TMPDIR environment variable should still be
  respected, e.g. to direct temporary files into a separate directory outside of
  the webserver’s document root but on the same file system.

* On POSIX operating systems, the
  [`fsync`](https://manpages.debian.org/stretch/manpages-dev/fsync.2) system
  call must be used to ensure that the `os.Rename()` call will not result in a
  0-length file.

This package attempts to get all of these details right, provides an intuitive,
yet flexible API and caters to use-cases where high performance is required.

## Disclaimer

This is not an official Google product (experimental or otherwise), it
is just code that happens to be owned by Google.

This project is not affiliated with the Go project.
