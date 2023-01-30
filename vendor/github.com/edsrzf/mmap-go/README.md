mmap-go
=======
![Build Status](https://github.com/edsrzf/mmap-go/actions/workflows/build-test.yml/badge.svg)
[![Go Reference](https://pkg.go.dev/badge/github.com/edsrzf/mmap-go.svg)](https://pkg.go.dev/github.com/edsrzf/mmap-go)

mmap-go is a portable mmap package for the [Go programming language](http://golang.org).

Operating System Support
========================
This package is tested using GitHub Actions on Linux, macOS, and Windows. It should also work on other Unix-like platforms, but hasn't been tested with them. I'm interested to hear about the results.

I haven't been able to add more features without adding significant complexity, so mmap-go doesn't support `mprotect`, `mincore`, and maybe a few other things. If you're running on a Unix-like platform and need some of these features, I suggest Gustavo Niemeyer's [gommap](http://labix.org/gommap).

This package compiles on Plan 9, but its functions always return errors.
