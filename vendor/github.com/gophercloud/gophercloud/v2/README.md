# Gophercloud: an OpenStack SDK for Go
[![Coverage Status](https://coveralls.io/repos/github/gophercloud/gophercloud/badge.svg?branch=master)](https://coveralls.io/github/gophercloud/gophercloud?branch=master)

Gophercloud is a Go SDK for OpenStack.

Join us on kubernetes slack, on [#gophercloud](https://kubernetes.slack.com/archives/C05G4NJ6P6X). Visit [slack.k8s.io](https://slack.k8s.io) for an invitation.

> **Note**
> This branch contains the current stable branch of Gophercloud: `v2`.
> The legacy stable version can be found in the `v1` branch.

## How to install

Reference a Gophercloud package in your code:

```go
import "github.com/gophercloud/gophercloud/v2"
```

Then update your `go.mod`:

```shell
go mod tidy
```

## Getting started

### Credentials

Because you'll be hitting an API, you will need to retrieve your OpenStack
credentials and either store them in a `clouds.yaml` file, as environment
variables, or in your local Go files. The first method is recommended because
it decouples credential information from source code, allowing you to push the
latter to your version control system without any security risk.

You will need to retrieve the following:

* A valid Keystone identity URL
* Credentials. These can be a username/password combo, a set of Application
  Credentials, a pre-generated token, or any other supported authentication
  mechanism.

For users who have the OpenStack dashboard installed, there's a shortcut. If
you visit the `project/api_access` path in Horizon and click on the
"Download OpenStack RC File" button at the top right hand corner, you can
download either a `clouds.yaml` file or an `openrc` bash file that exports all
of your access details to environment variables. To use the `clouds.yaml` file,
place it at `~/.config/openstack/clouds.yaml`. To use the `openrc` file, run
`source openrc` and you will be prompted for your password.

### Gophercloud authentication

Gophercloud authentication is organized into two layered abstractions:
* `ProviderClient` holds the authentication token and can be used to build a
  `ServiceClient`.
* `ServiceClient` specializes against one specific OpenStack module and can
  directly be used to make API calls.

A provider client is a top-level client that all of your OpenStack service
clients derive from. The provider contains all of the authentication details
that allow your Go code to access the API - such as the base URL and token ID.

One single Provider client can be used to build as many Service clients as needed.

**With `clouds.yaml`**

```go
package main

import (
	"context"

	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/config"
	"github.com/gophercloud/gophercloud/v2/openstack/config/clouds"
)

func main() {
	ctx := context.Background()

	// Fetch coordinates from a `cloud.yaml` in the current directory, or
	// in the well-known config directories (different for each operating
	// system).
	authOptions, endpointOptions, tlsConfig, err := clouds.Parse()
	if err != nil {
		panic(err)
	}

	// Call Keystone to get an authentication token, and use it to
	// construct a ProviderClient. All functions hitting the OpenStack API
	// accept a `context.Context` to enable tracing and cancellation.
	providerClient, err := config.NewProviderClient(ctx, authOptions, config.WithTLSConfig(tlsConfig))
	if err != nil {
		panic(err)
	}

	// Use the ProviderClient and the endpoint options fetched from
	// `clouds.yaml` to build a service client: a compute client in this
	// case. Note that the contructor does not accept a `context.Context`:
	// no further call to the OpenStack API is needed at this stage.
	computeClient, err := openstack.NewComputeV2(providerClient, endpointOptions)
	if err != nil {
		panic(err)
	}

	// use the computeClient
}
```

**With environment variables (`openrc`)**

Gophercloud can parse the environment variables set by running `source openrc`:

```go
package main

import (
	"context"
	"os"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
)

func main() {
	ctx := context.Background()

	opts, err := openstack.AuthOptionsFromEnv()
	if err != nil {
		panic(err)
	}

	providerClient, err := openstack.AuthenticatedClient(ctx, opts)
	if err != nil {
		panic(err)
	}

	computeClient, err := openstack.NewComputeV2(providerClient, gophercloud.EndpointOpts{
		Region: os.Getenv("OS_REGION_NAME"),
	})
	if err != nil {
		panic(err)
	}

	// use the computeClient
}
```

**Manually**

You can also generate a "Provider" by passing in your credentials
explicitly:

```go
package main

import (
	"context"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
)

func main() {
	ctx := context.Background()

	providerClient, err := openstack.AuthenticatedClient(ctx, gophercloud.AuthOptions{
		IdentityEndpoint: "https://openstack.example.com:5000/v2.0",
		Username:         "username",
		Password:         "password",
	})
	if err != nil {
		panic(err)
	}

	computeClient, err := openstack.NewComputeV2(providerClient, gophercloud.EndpointOpts{
		Region: "RegionName",
	})
	if err != nil {
		panic(err)
	}

	// use the computeClient
}
```

### Provision a server

We can use the Compute service client generated above for any Compute API
operation we want. In our case, we want to provision a new server. To do this,
we invoke the `Create` method and pass in the flavor ID (hardware
specification) and image ID (operating system) we're interested in:

```go
import "github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"

func main() {
    // [...]

    server, err := servers.Create(context.TODO(), computeClient, servers.CreateOpts{
        Name:      "My new server!",
        FlavorRef: "flavor_id",
        ImageRef:  "image_id",
    }).Extract()

    // [...]
```

The above code sample creates a new server with the parameters, and returns a
[`servers.Server`](https://pkg.go.dev/github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers#Server).

## Supported Services

|        **Service**       | **Name**         |             **Module**             | **1.x** | **2.x** |
|:------------------------:|------------------|:----------------------------------:|:-------:|:-------:|
|         Baremetal        |      Ironic      |        `openstack/baremetal`       |    ✔    |    ✔    |
|  Baremetal Introspection | Ironic Inspector | `openstack/baremetalintrospection` |    ✔    |    ✔    |
|       Block Storage      |      Cinder      |      `openstack/blockstorage`      |    ✔    |    ✔    |
|        Clustering        |      Senlin      |       `openstack/clustering`       |    ✔    |    ✘    |
|          Compute         |       Nova       |         `openstack/compute`        |    ✔    |    ✔    |
|         Container        |        Zun       |        `openstack/container`       |    ✔    |    ✔    |
| Container Infrastructure |      Magnum      |     `openstack/containerinfra`     |    ✔    |    ✔    |
|         Database         |       Trove      |           `openstack/db`           |    ✔    |    ✔    |
|            DNS           |     Designate    |           `openstack/dns`          |    ✔    |    ✔    |
|         Identity         |     Keystone     |        `openstack/identity`        |    ✔    |    ✔    |
|           Image          |      Glance      |          `openstack/image`         |    ✔    |    ✔    |
|      Key Management      |     Barbican     |       `openstack/keymanager`       |    ✔    |    ✔    |
|      Load Balancing      |      Octavia     |      `openstack/loadbalancer`      |    ✔    |    ✔    |
|         Messaging        |       Zaqar      |        `openstack/messaging`       |    ✔    |    ✔    |
|        Networking        |      Neutron     |       `openstack/networking`       |    ✔    |    ✔    |
|      Object Storage      |       Swift      |      `openstack/objectstorage`     |    ✔    |    ✔    |

## Advanced Usage

Have a look at the [FAQ](./docs/FAQ.md) for some tips on customizing the way Gophercloud works.

## Backwards-Compatibility Guarantees

Gophercloud versioning follows [semver](https://semver.org/spec/v2.0.0.html).

Before `v1.0.0`, there were no guarantees. Starting with v1, there will be no breaking changes within a major release.

See the [Release instructions](./RELEASE.md).

## Contributing

See the [contributing guide](./.github/CONTRIBUTING.md).

## Help and feedback

If you're struggling with something or have spotted a potential bug, feel free
to submit an issue to our [bug tracker](https://github.com/gophercloud/gophercloud/issues).
