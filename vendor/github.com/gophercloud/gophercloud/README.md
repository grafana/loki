# Gophercloud: an OpenStack SDK for Go
[![Build Status](https://travis-ci.org/gophercloud/gophercloud.svg?branch=master)](https://travis-ci.org/gophercloud/gophercloud)
[![Coverage Status](https://coveralls.io/repos/github/gophercloud/gophercloud/badge.svg?branch=master)](https://coveralls.io/github/gophercloud/gophercloud?branch=master)

Gophercloud is an OpenStack Go SDK.

## Useful links

* [Reference documentation](http://godoc.org/github.com/gophercloud/gophercloud)
* [Effective Go](https://golang.org/doc/effective_go.html)

## How to install

Reference a Gophercloud package in your code:

```Go
import "github.com/gophercloud/gophercloud"
```

Then update your `go.mod`:

```shell
go mod tidy
```

## Getting started

### Credentials

Because you'll be hitting an API, you will need to retrieve your OpenStack
credentials and either store them as environment variables or in your local Go
files. The first method is recommended because it decouples credential
information from source code, allowing you to push the latter to your version
control system without any security risk.

You will need to retrieve the following:

* username
* password
* a valid Keystone identity URL

For users that have the OpenStack dashboard installed, there's a shortcut. If
you visit the `project/access_and_security` path in Horizon and click on the
"Download OpenStack RC File" button at the top right hand corner, you will
download a bash file that exports all of your access details to environment
variables. To execute the file, run `source admin-openrc.sh` and you will be
prompted for your password.

### Authentication

> NOTE: It is now recommended to use the `clientconfig` package found at
> https://github.com/gophercloud/utils/tree/master/openstack/clientconfig
> for all authentication purposes.
>
> The below documentation is still relevant. clientconfig simply implements
> the below and presents it in an easier and more flexible way.

Once you have access to your credentials, you can begin plugging them into
Gophercloud. The next step is authentication, and this is handled by a base
"Provider" struct. To get one, you can either pass in your credentials
explicitly, or tell Gophercloud to use environment variables:

```go
import (
  "github.com/gophercloud/gophercloud"
  "github.com/gophercloud/gophercloud/openstack"
  "github.com/gophercloud/gophercloud/openstack/utils"
)

// Option 1: Pass in the values yourself
opts := gophercloud.AuthOptions{
  IdentityEndpoint: "https://openstack.example.com:5000/v2.0",
  Username: "{username}",
  Password: "{password}",
}

// Option 2: Use a utility function to retrieve all your environment variables
opts, err := openstack.AuthOptionsFromEnv()
```

Once you have the `opts` variable, you can pass it in and get back a
`ProviderClient` struct:

```go
provider, err := openstack.AuthenticatedClient(opts)
```

The `ProviderClient` is the top-level client that all of your OpenStack services
derive from. The provider contains all of the authentication details that allow
your Go code to access the API - such as the base URL and token ID.

### Provision a server

Once we have a base Provider, we inject it as a dependency into each OpenStack
service. In order to work with the Compute API, we need a Compute service
client; which can be created like so:

```go
client, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
  Region: os.Getenv("OS_REGION_NAME"),
})
```

We then use this `client` for any Compute API operation we want. In our case,
we want to provision a new server - so we invoke the `Create` method and pass
in the flavor ID (hardware specification) and image ID (operating system) we're
interested in:

```go
import "github.com/gophercloud/gophercloud/openstack/compute/v2/servers"

server, err := servers.Create(client, servers.CreateOpts{
  Name:      "My new server!",
  FlavorRef: "flavor_id",
  ImageRef:  "image_id",
}).Extract()
```

The above code sample creates a new server with the parameters, and embodies the
new resource in the `server` variable (a
[`servers.Server`](http://godoc.org/github.com/gophercloud/gophercloud) struct).

## Advanced Usage

Have a look at the [FAQ](./docs/FAQ.md) for some tips on customizing the way Gophercloud works.

## Backwards-Compatibility Guarantees

Gophercloud versioning follows [semver](https://semver.org/spec/v2.0.0.html).

Before `v1.0.0`, there were no guarantees. Starting with v1, there will be no breaking changes within a major release.

## Contributing

See the [contributing guide](./.github/CONTRIBUTING.md).

## Help and feedback

If you're struggling with something or have spotted a potential bug, feel free
to submit an issue to our [bug tracker](https://github.com/gophercloud/gophercloud/issues).

## Thank You

We'd like to extend special thanks and appreciation to the following:

### OpenLab

<a href="http://openlabtesting.org/"><img src="./docs/assets/openlab.png" width="600px"></a>

OpenLab is providing a full CI environment to test each PR and merge for a variety of OpenStack releases.

### VEXXHOST

<a href="https://vexxhost.com/"><img src="./docs/assets/vexxhost.png" width="600px"></a>

VEXXHOST is providing their services to assist with the development and testing of Gophercloud.
