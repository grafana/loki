# Godo

[![GitHub Actions CI](https://github.com/digitalocean/godo/actions/workflows/ci.yml/badge.svg)](https://github.com/digitalocean/godo/actions/workflows/ci.yml)
[![GoDoc](https://godoc.org/github.com/digitalocean/godo?status.svg)](https://godoc.org/github.com/digitalocean/godo)

Godo is a Go client library for accessing the DigitalOcean V2 API.

You can view the client API docs here: [http://godoc.org/github.com/digitalocean/godo](http://godoc.org/github.com/digitalocean/godo)

You can view DigitalOcean API docs here: [https://docs.digitalocean.com/reference/api/api-reference/](https://docs.digitalocean.com/reference/api/api-reference/)

> **🚀 New in v1.191.0 — AI & Inference support**
>
> `godo` now ships first-class support for DigitalOcean's
> [DigitalOcean Inference](https://docs.digitalocean.com/products/inference/): chat
> completions (with streaming), image generation, embeddings, batch inference,
> model listing, and more — all from the same `Client`. Jump to
> [**AI & Inference**](#ai--inference) to get started.

## Install
```sh
go get github.com/digitalocean/godo@vX.Y.Z
```

where X.Y.Z is the [version](https://github.com/digitalocean/godo/releases) you need.

or
```sh
go get github.com/digitalocean/godo
```
for non Go modules usage or latest version.

## Usage

```go
import "github.com/digitalocean/godo"
```

Create a new DigitalOcean client, then use the exposed services to
access different parts of the DigitalOcean API.

### Authentication

You can manage API tokens at the DigitalOcean Control Panel
[Applications Page](https://cloud.digitalocean.com/settings/applications).

```go
package main

import (
    "github.com/digitalocean/godo"
)

func main() {
    client := godo.NewFromToken("my-digitalocean-api-token")
}
```

> **Credentials for inference APIs**
>
> What matters is the **credential** you pass to `godo.NewFromToken`, not which
> DigitalOcean API you call:
>
> | What you're calling | What you need |
> | --- | --- |
> | Infrastructure APIs (`Droplets`, `Kubernetes`, `Volumes`, …) | A DigitalOcean API token (PAT). |
> | Inference APIs (`Chat`, `Models`, `Embeddings`, `ImageGenerations`, `Messages`, `Responses`, `BatchInference`, …) | A PAT created with **full access** scope, **or** a **model access key**. |
>
> If you only have a limited-scope PAT, infrastructure calls will work but
> inference calls will fail with `401`. Create a new PAT with full access, or use
> a Model Access Key instead.
>
> ```go
> // Either credential works with godo.NewFromToken:
> client := godo.NewFromToken(os.Getenv("DIGITALOCEAN_TOKEN"))  // full-access PAT
> client := godo.NewFromToken(os.Getenv("MODEL_ACCESS_KEY"))    // model access key
> ```

If you need to provide a `context.Context` to your new client, you should use [`godo.NewClient`](https://godoc.org/github.com/digitalocean/godo#NewClient) to manually construct a client instead.

## Action Gateway

`client.ActionGateway` exposes the Action Gateway control-plane resources: tools,
toolbelts, output views, MCP servers, connections, sessions, users, and end-user
limits. Use a PAT with the corresponding `action_gateway` scopes. For example:

```go
toolbelts, _, err := client.ActionGateway.Toolbelts.List(ctx, &godo.ActionGatewayToolbeltsListOptions{Status: "active"})
if err != nil { return err }
fmt.Println(toolbelts.Toolbelts)

session, _, err := client.ActionGateway.Sessions.CreateRuntime(ctx, &godo.ActionGatewaySessionCreateRequest{
    ActorID: "alice",
})
if err != nil { return err }
tools, _, err := session.ChatTools(ctx, nil) // action_search, action_invoke, action_code
if err != nil { return err }
completion, _, err := client.Chat.Completions.New(ctx, &godo.ChatCompletionNewParams{
    Model: "llama3.3-70b-instruct", Messages: []godo.ChatCompletionMessage{godo.UserMessage("Find recent news")}, Tools: tools,
})
if err != nil { return err }
toolMessages, _, err := session.HandleChatToolCalls(ctx, completion)
if err != nil { return err }
fmt.Println(toolMessages)
```

`session.Tools` supports direct discovery and invocation, while `session.Code`
runs sandboxed code. `MessageTools`/`HandleMessageToolCalls` and
`ResponseTools`/`HandleResponseToolCalls` integrate with the other inference
formats. `CreateRuntime` defaults to an ask-before-executing policy; provide an
explicit policy to change it. Omitted `Tools` enables all tools, while an empty
non-nil slice enables none. MCP calls accept only HTTPS session URLs on
`actions.do-ai.run`; `WithActionGatewayMCPBaseURL` overrides the origin for
development or tests.

## AI & Inference

> Talk to models on [DigitalOcean Inference](https://docs.digitalocean.com/products/inference/) with the same `godo.Client`.

The [Serverless Inference API](https://docs.digitalocean.com/reference/api/reference/serverless-inference/) is available at `https://inference.do-ai.run/`. Use a **DigitalOcean PAT with full access scope** or a **model access key** — see the [credentials note](#authentication) above.

#### Chat completion

```go
completion, _, err := client.Chat.Completions.New(ctx, &godo.ChatCompletionNewParams{
    Model: "llama3.3-70b-instruct",
    Messages: []godo.ChatCompletionMessage{
        godo.UserMessage("Write me a haiku"),
    },
})
```

#### List models

```go
page, _, err := client.Models.List(ctx)
for _, m := range page.Data {
    fmt.Println(m.ID)
}
```

#### Image generation

```go
image, _, err := client.ImageGenerations.Generate(ctx, &godo.ImageGenerateParams{
    Model:  "stable-diffusion-3.5-large",
    Prompt: "A friendly cartoon shark typing on a laptop at a sunny beach",
    N:      1,
})
```

For streaming, embeddings, messages, responses, async invocations, batch inference, agent inference, and full runnable programs, see [`examples/serverless-inference/`](examples/serverless-inference/) and [`examples/agent-inference/`](examples/agent-inference/).

## Examples


To create a new Droplet:

```go
dropletName := "super-cool-droplet"

createRequest := &godo.DropletCreateRequest{
    Name:   dropletName,
    Region: "nyc3",
    Size:   "s-1vcpu-1gb",
    Image: godo.DropletCreateImage{
        Slug: "ubuntu-20-04-x64",
    },
}

ctx := context.TODO()

newDroplet, _, err := client.Droplets.Create(ctx, createRequest)

if err != nil {
    fmt.Printf("Something bad happened: %s\n\n", err)
    return err
}
```

### Pagination

If a list of items is paginated by the API, you must request pages individually. For example, to fetch all Droplets:

```go
func DropletList(ctx context.Context, client *godo.Client) ([]godo.Droplet, error) {
    // create a list to hold our droplets
    list := []godo.Droplet{}

    // create options. initially, these will be blank
    opt := &godo.ListOptions{}
    for {
        droplets, resp, err := client.Droplets.List(ctx, opt)
        if err != nil {
            return nil, err
        }

        // append the current page's droplets to our list
        list = append(list, droplets...)

        // if we are at the last page, break out the for loop
        if resp.Links == nil || resp.Links.IsLastPage() {
            break
        }

        page, err := resp.Links.CurrentPage()
        if err != nil {
            return nil, err
        }

        // set the page we want for the next request
        opt.Page = page + 1
    }

    return list, nil
}
```

Some endpoints offer token based pagination. For example, to fetch all Registry Repositories:

```go
func ListRepositoriesV2(ctx context.Context, client *godo.Client, registryName string) ([]*godo.RepositoryV2, error) {
    // create a list to hold our registries
    list := []*godo.RepositoryV2{}

    // create options. initially, these will be blank
    opt := &godo.TokenListOptions{}
    for {
        repositories, resp, err := client.Registry.ListRepositoriesV2(ctx, registryName, opt)
        if err != nil {
            return nil, err
        }

        // append the current page's registries to our list
        list = append(list, repositories...)

        // if we are at the last page, break out the for loop
        if resp.Links == nil || resp.Links.IsLastPage() {
            break
        }

        // grab the next page token
        nextPageToken, err := resp.Links.NextPageToken()
        if err != nil {
            return nil, err
        }

        // provide the next page token for the next request
        opt.Token = nextPageToken
    }

    return list, nil
}
```

### Automatic Retries and Exponential Backoff

The Godo client can be configured to use automatic retries and exponentional backoff for requests that fail with 429 or 500-level response codes via [go-retryablehttp](https://github.com/hashicorp/go-retryablehttp). To configure Godo to enable usage of go-retryablehttp, the `RetryConfig.RetryMax` must be set.

```go
tokenSrc := oauth2.StaticTokenSource(&oauth2.Token{
    AccessToken: "dop_v1_xxxxxx",
})

oauth_client := oauth2.NewClient(oauth2.NoContext, tokenSrc)

waitMax := godo.PtrTo(6.0)
waitMin := godo.PtrTo(3.0)

retryConfig := godo.RetryConfig{
    RetryMax:     3,
    RetryWaitMin: waitMin,
    RetryWaitMax: waitMax,
}

client, err := godo.New(oauth_client, godo.WithRetryAndBackoffs(retryConfig))
```

Please refer to the [RetryConfig Godo documentation](https://pkg.go.dev/github.com/digitalocean/godo#RetryConfig) for more information.

## Versioning

Each version of the client is tagged and the version is updated accordingly.

To see the list of past versions, run `git tag`.


## Documentation

For a comprehensive list of examples, check out the [API documentation](https://docs.digitalocean.com/reference/api/api-reference/#tag/SSH-Keys).

For details on all the functionality in this library, see the [GoDoc](http://godoc.org/github.com/digitalocean/godo) documentation.


## Contributing

We love pull requests! Please see the [contribution guidelines](CONTRIBUTING.md).
