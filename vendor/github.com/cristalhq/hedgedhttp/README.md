# hedgedhttp

[![build-img]][build-url]
[![pkg-img]][pkg-url]
[![reportcard-img]][reportcard-url]
[![coverage-img]][coverage-url]
[![version-img]][version-url]

Hedged HTTP client which helps to reduce tail latency at scale.

## Rationale

See paper [Tail at Scale](https://cacm.acm.org/magazines/2013/2/160173-the-tail-at-scale/fulltext) by Jeffrey Dean, Luiz André Barroso. In short: the client first sends one request, but then sends an additional request after a timeout if the previous hasn't returned an answer in the expected time. The client cancels remaining requests once the first result is received.

## Acknowledge

Thanks to [Bohdan Storozhuk](https://github.com/storozhukbm) for the review and powerful hints.

## Features

* Simple API.
* Easy to integrate.
* Optimized for speed.
* Clean and tested code.
* Supports `http.Client` and `http.RoundTripper`.
* Dependency-free.

## Install

Go version 1.16+

```
go get github.com/cristalhq/hedgedhttp
```

## Example

```go
ctx := context.Background()
req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://google.com", http.NoBody)
if err != nil {
	panic(err)
}

timeout := 10 * time.Millisecond
upto := 7
client := &http.Client{Timeout: time.Second}
hedged, err := hedgedhttp.NewClient(timeout, upto, client)
if err != nil {
	panic(err)
}

// will take `upto` requests, with a `timeout` delay between them
resp, err := hedged.Do(req)
if err != nil {
	panic(err)
}
defer resp.Body.Close()
```

Also see examples: [examples_test.go](https://github.com/cristalhq/hedgedhttp/blob/main/examples_test.go).

## Documentation

See [these docs][pkg-url].

## License

[MIT License](LICENSE).

[build-img]: https://github.com/cristalhq/hedgedhttp/workflows/build/badge.svg
[build-url]: https://github.com/cristalhq/hedgedhttp/actions
[pkg-img]: https://pkg.go.dev/badge/cristalhq/hedgedhttp
[pkg-url]: https://pkg.go.dev/github.com/cristalhq/hedgedhttp
[reportcard-img]: https://goreportcard.com/badge/cristalhq/hedgedhttp
[reportcard-url]: https://goreportcard.com/report/cristalhq/hedgedhttp
[coverage-img]: https://codecov.io/gh/cristalhq/hedgedhttp/branch/main/graph/badge.svg
[coverage-url]: https://codecov.io/gh/cristalhq/hedgedhttp
[version-img]: https://img.shields.io/github/v/release/cristalhq/hedgedhttp
[version-url]: https://github.com/cristalhq/hedgedhttp/releases
