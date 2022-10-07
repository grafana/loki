# Go gRPC Middleware V2

[![Travis Build](https://travis-ci.org/grpc-ecosystem/go-grpc-middleware.svg?branch=master)](https://travis-ci.org/grpc-ecosystem/go-grpc-middleware)
[![Go Report Card](https://goreportcard.com/badge/github.com/grpc-ecosystem/go-grpc-middleware)](https://goreportcard.com/report/github.com/grpc-ecosystem/go-grpc-middleware)
[![GoDoc](http://img.shields.io/badge/GoDoc-Reference-blue.svg)](https://godoc.org/github.com/grpc-ecosystem/go-grpc-middleware)
[![SourceGraph](https://sourcegraph.com/github.com/grpc-ecosystem/go-grpc-middleware/-/badge.svg)](https://sourcegraph.com/github.com/grpc-ecosystem/go-grpc-middleware/?badge)
[![codecov](https://codecov.io/gh/grpc-ecosystem/go-grpc-middleware/branch/master/graph/badge.svg)](https://codecov.io/gh/grpc-ecosystem/go-grpc-middleware)
[![Apache 2.0 License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![quality: production](https://img.shields.io/badge/quality-production-orange.svg)](#status)
[![Slack](https://img.shields.io/badge/slack-%23grpc--middleware-brightgreen)](https://slack.com/share/IRUQCFC23/9Tm7hxRFVKKNoajQfMOcUiIk/enQtODc4ODI4NTIyMDcxLWM5NDA0ZTE4Njg5YjRjYWZkMTI5MzQwNDY3YzBjMzE1YzdjOGM5ZjI1NDNiM2JmNzI2YjM5ODE5OTRiNTEyOWE)

[gRPC Go](https://github.com/grpc/grpc-go) Middleware: interceptors, helpers, utilities.

**NOTE: V2 is under development. If you want to be up to date, or better (!) help us improve go-grpc-middleware please follow on https://github.com/grpc-ecosystem/go-grpc-middleware/issues/275.**

## Middleware

[gRPC Go](https://github.com/grpc/grpc-go) recently acquired support for
Interceptors, i.e. [middleware](https://medium.com/@matryer/writing-middleware-in-golang-and-how-go-makes-it-so-much-fun-4375c1246e81#.gv7tdlghs)
that is executed either on the gRPC Server before the request is passed onto the user's application logic, or on the gRPC client either around the user call. It is a perfect way to implement
common patterns: auth, logging, message, validation, retries or monitoring.

These are generic building blocks that make it easy to build multiple microservices easily.
The purpose of this repository is to act as a go-to point for such reusable functionality. It contains
some of them itself, but also will link to useful external repos.

`middleware` itself provides support for chaining interceptors, here's an example:

```go
import "github.com/grpc-ecosystem/go-grpc-middleware/v2"

myServer := grpc.NewServer(
    grpc.StreamInterceptor(middleware.ChainStreamServer(
        tags.StreamServerInterceptor(),
        opentracing.StreamServerInterceptor(),
        prometheus.StreamServerInterceptor,
        zap.StreamServerInterceptor(zapLogger),
        auth.StreamServerInterceptor(myAuthFunction),
        recovery.StreamServerInterceptor(),
    )),
    grpc.UnaryInterceptor(middleware.ChainUnaryServer(
        tags.UnaryServerInterceptor(),
        opentracing.UnaryServerInterceptor(),
        prometheus.UnaryServerInterceptor,
        zap.UnaryServerInterceptor(zapLogger),
        auth.UnaryServerInterceptor(myAuthFunction),
        recovery.UnaryServerInterceptor(),
    )),
)
```

## Interceptors

*Please send a PR to add new interceptors or middleware to this list*

#### Auth
   * [`auth`](auth) - a customizable (via `AuthFunc`) piece of auth middleware

#### Logging
   * [`tags`](interceptors/tags) - a library that adds a `Tag` map to context, with data populated from request body
   * [`zap`](providers/zap) - integration of [zap](https://github.com/uber-go/zap) logging library into gRPC handlers.
   * [`logrus`](providers/logrus) - integration of [logrus](https://github.com/sirupsen/logrus) logging library into gRPC handlers.
   * [`kit`](providers/kit) - integration of [go-kit](https://github.com/go-kit/kit/tree/master/log) logging library into gRPC handlers.
   * [`zerolog`](providers/zerolog) - integration of [zerolog](https://github.com/rs/zerolog) logging Library into gRPC handlers.

#### Monitoring
   * [`grpc_prometheus`⚡](https://github.com/grpc-ecosystem/go-grpc-prometheus) - Prometheus client-side and server-side monitoring middleware
   * [`opentracing`](interceptors/tracing) - [OpenTracing](http://opentracing.io/) client-side and server-side interceptors with support for streaming and handler-returned tags

#### Client
   * [`retry`](interceptors/retry) - a generic gRPC response code retry mechanism, client-side middleware

#### Server
   * [`validator`](interceptors/validator) - codegen inbound message validation from `.proto` options
   * [`recovery`](interceptors/recovery) - turn panics into gRPC errors
   * [`ratelimit`](interceptors/ratelimit) - grpc rate limiting by your own limiter


## Status

This code has been running in *production* since May 2016 as the basis of the gRPC micro services stack at [Improbable](https://improbable.io).

Additional tooling will be added, and contributions are welcome.

## License

`go-grpc-middleware` is released under the Apache 2.0 license. See the [LICENSE](LICENSE) file for details.
