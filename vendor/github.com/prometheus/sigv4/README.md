github.com/prometheus/sigv4 module
=========================================

[![Go Reference](https://pkg.go.dev/badge/github.com/prometheus/sigv4.svg)](https://pkg.go.dev/github.com/prometheus/sigv4)
[![Build Status](https://github.com/prometheus/sigv4/actions/workflows/ci.yml/badge.svg)](https://github.com/prometheus/sigv4/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/prometheus/sigv4)](https://goreportcard.com/report/github.com/prometheus/sigv4)

sigv4 provides a http.RoundTripper that will sign requests using
Amazon's Signature Verification V4 signing procedure, using credentials
from the default AWS credential chain.

This is a separate module from github.com/prometheus/common to prevent
it from having and propagating a dependency on the AWS SDK.

This module is considered internal to Prometheus, without any stability
guarantees for external usage.
