# Microsoft Azure Event Hubs Client for Golang
[![Go Report Card](https://goreportcard.com/badge/github.com/Azure/azure-event-hubs-go)](https://goreportcard.com/report/github.com/Azure/azure-event-hubs-go)
[![godoc](https://godoc.org/github.com/Azure/azure-event-hubs-go?status.svg)](https://godoc.org/github.com/Azure/azure-event-hubs-go)
[![Build Status](https://travis-ci.org/Azure/azure-event-hubs-go.svg?branch=master)](https://travis-ci.org/Azure/azure-event-hubs-go)
[![Coverage Status](https://coveralls.io/repos/github/Azure/azure-event-hubs-go/badge.svg?branch=master)](https://coveralls.io/github/Azure/azure-event-hubs-go?branch=master)

Azure Event Hubs is a highly scalable publish-subscribe service that can ingest millions of events per second and 
stream them into multiple applications. This lets you process and analyze the massive amounts of data produced by your 
connected devices and applications. Once Event Hubs has collected the data, you can retrieve, transform and store it by 
using any real-time analytics provider or with batching/storage adapters. 

Refer to the [online documentation](https://azure.microsoft.com/services/event-hubs/) to learn more about Event Hubs in 
general.

This library is a pure Golang implementation of Azure Event Hubs over AMQP.

## Install with Go modules
If you want to use stable versions of the library, please use Go modules.

**NOTE**: versions prior to 3.0.0 depend on pack.ag/amqp which is no longer maintained. Any new code should not use versions prior to 3.0.0.

### Using go get targeting version 3.x.x
``` bash
go get -u github.com/Azure/azure-event-hubs-go/v3
```

### Using go get targeting version 2.x.x
``` bash
go get -u github.com/Azure/azure-event-hubs-go/v2
```

### Using go get targeting version 1.x.x
``` bash
go get -u github.com/Azure/azure-event-hubs-go
```

## Using Event Hubs
In this section we'll cover some basics of the library to help you get started.

This library has two main dependencies, [vcabbage/amqp](https://github.com/vcabbage/amqp) and 
[Azure AMQP Common](https://github.com/Azure/azure-amqp-common-go). The former provides the AMQP protocol implementation
and the latter provides some common authentication, persistence and request-response message flows.

### Quick start
Let's send and receive `"hello, world!"` to all the partitions in an Event Hub.
```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"
	
	"github.com/Azure/azure-event-hubs-go/v3"
)

func main() {
	connStr := "Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=superSecret1234=;EntityPath=hubName"
	hub, err := eventhub.NewHubFromConnectionString(connStr)

	if err != nil {
		fmt.Println(err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// send a single message into a random partition
	err = hub.Send(ctx, eventhub.NewEventFromString("hello, world!"))
	if err != nil {
		fmt.Println(err)
		return
	}

	handler := func(c context.Context, event *eventhub.Event) error {
		fmt.Println(string(event.Data))
		return nil
	}

	// listen to each partition of the Event Hub
	runtimeInfo, err := hub.GetRuntimeInformation(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	
	for _, partitionID := range runtimeInfo.PartitionIDs { 
		// Start receiving messages 
		// 
		// Receive blocks while attempting to connect to hub, then runs until listenerHandle.Close() is called 
		// <- listenerHandle.Done() signals listener has stopped
		// listenerHandle.Err() provides the last error the receiver encountered 
		listenerHandle, err := hub.Receive(ctx, partitionID, handler, eventhub.ReceiveWithLatestOffset())
		if err != nil {
			fmt.Println(err)
			return
		}
    }

	// Wait for a signal to quit:
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, os.Kill)
	<-signalChan

	err = hub.Close(context.Background())
	if err != nil {
		fmt.Println(err)
	}
}
```

### Environment Variables
In the above example, the `Hub` instance was created using environment variables. Here is a list of environment 
variables used in this project.

#### Event Hub env vars
- `EVENTHUB_NAMESPACE` the namespace of the Event Hub instance
- `EVENTHUB_NAME` the name of the Event Hub instance

#### SAS TokenProvider environment variables:
There are two sets of environment variables which can produce a SAS TokenProvider
1) Expected Environment Variables:
    - `EVENTHUB_KEY_NAME` the name of the Event Hub key
    - `EVENTHUB_KEY_VALUE` the secret for the Event Hub key named in `EVENTHUB_KEY_NAME`

2) Expected Environment Variable:
    - `EVENTHUB_CONNECTION_STRING` connection string from the Azure portal like: `Endpoint=sb://foo.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=fluffypuppy;EntityPath=hubName`

#### AAD TokenProvider environment variables:
1) Client Credentials: attempt to authenticate with a [Service Principal](https://docs.microsoft.com/en-us/azure/azure-resource-manager/resource-group-create-service-principal-portal) via
    - `AZURE_TENANT_ID` the Azure Tenant ID
    - `AZURE_CLIENT_ID` the Azure Application ID
    - `AZURE_CLIENT_SECRET` a key / secret for the corresponding application
2) Client Certificate: attempt to authenticate with a Service Principal via 
    - `AZURE_TENANT_ID` the Azure Tenant ID
    - `AZURE_CLIENT_ID` the Azure Application ID
    - `AZURE_CERTIFICATE_PATH` the path to the certificate file
    - `AZURE_CERTIFICATE_PASSWORD` the password for the certificate

The Azure Environment used can be specified using the name of the Azure Environment set in "AZURE_ENVIRONMENT" var.

### Authentication
Event Hubs offers a couple different paths for authentication, shared access signatures (SAS) and Azure Active Directory (AAD)
JWT authentication. Both token types are available for use and are exposed through the `TokenProvider` interface.
```go
// TokenProvider abstracts the fetching of authentication tokens
TokenProvider interface {
    GetToken(uri string) (*Token, error)
}
```

#### SAS token provider
The SAS token provider uses the namespace of the Event Hub, the name of the "Shared access policy" key and the value of
the key to produce a token.

You can create new Shared access policies through the Azure portal as shown below.
![SAS policies in the Azure portal](./_content/sas-policy.png)

You can create a SAS token provider in a couple different ways. You can build one with a key name and key value like 
this.
```go
provider := sas.TokenProviderWithKey("myKeyName", "myKeyValue")
```

Or, you can create a token provider from environment variables like this.
```go
// TokenProviderWithEnvironmentVars creates a new SAS TokenProvider from environment variables
//
// There are two sets of environment variables which can produce a SAS TokenProvider
//
// 1) Expected Environment Variables:
//   - "EVENTHUB_KEY_NAME" the name of the Event Hub key
//   - "EVENTHUB_KEY_VALUE" the secret for the Event Hub key named in "EVENTHUB_KEY_NAME"
//
// 2) Expected Environment Variable:
//   - "EVENTHUB_CONNECTION_STRING" connection string from the Azure portal

provider, err := sas.NewTokenProvider(sas.TokenProviderWithEnvironmentVars())
```

#### AAD JWT token provider
The AAD JWT token provider uses Azure Active Directory to authenticate the service and acquire a token (JWT) which is
used to authenticate with Event Hubs. The authenticated identity must have `Contributor` role based authorization for
the Event Hub instance. [This article](https://docs.microsoft.com/en-us/azure/event-hubs/event-hubs-role-based-access-control) 
provides more information about this preview feature.

The easiest way to create a JWT token provider is via environment variables.
```go
// 1. Client Credentials: attempt to authenticate with a Service Principal via "AZURE_TENANT_ID", "AZURE_CLIENT_ID" and
//    "AZURE_CLIENT_SECRET"
//
// 2. Client Certificate: attempt to authenticate with a Service Principal via "AZURE_TENANT_ID", "AZURE_CLIENT_ID",
//    "AZURE_CERTIFICATE_PATH" and "AZURE_CERTIFICATE_PASSWORD"
//
// 3. Managed Service Identity (MSI): attempt to authenticate via MSI
//
//
// The Azure Environment used can be specified using the name of the Azure Environment set in "AZURE_ENVIRONMENT" var.
provider, err := aad.NewJWTProvider(aad.JWTProviderWithEnvironmentVars())
```

You can also provide your own `adal.ServicePrincipalToken`.
```go
config := &aad.TokenProviderConfiguration{
    ResourceURI: azure.PublicCloud.ResourceManagerEndpoint,
    Env:         &azure.PublicCloud,
}

spToken, err := config.NewServicePrincipalToken()
if err != nil {
    // handle err
}
provider, err := aad.NewJWTProvider(aad.JWTProviderWithAADToken(aadToken))
```

### Send And Receive
The basics of messaging are sending and receiving messages. Here are the different ways you can do that.

#### Sending to a particular partition
By default, a Hub will send messages any of the load balanced partitions. Sometimes you want to send to only a 
particular partition. You can do this in two ways.
1) You can supply a partition key on an event
    ```go
    event := eventhub.NewEventFromString("foo")
    event.PartitionKey = "bazz"
    hub.Send(ctx, event) // send event to the partition ID to which partition key hashes
    ```
2) You can build a hub instance that will only send to one partition.
    ```go
    partitionID := "0"
    hub, err := eventhub.NewHubFromEnvironment(eventhub.HubWithPartitionedSender(partitionID))
    ```

#### Sending batches of events
Sending a batch of messages is more efficient than sending a single message. `SendBatch` takes an `*EventBatchIterator` that will automatically create batches from a slice of `*Event`.
```go
import (
    eventhub "github.com/Azure/azure-event-hubs-go/v3"
)
...
var events []*eventhub.Event
events = append(events, eventhub.NewEventFromString("one"))
events = append(events, eventhub.NewEventFromString("two"))
events = append(events, eventhub.NewEventFromString("three"))

err := client.SendBatch(ctx, eventhub.NewEventBatchIterator(events...))
```

#### Receiving
When receiving messages from an Event Hub, you always need to specify the partition you'd like to receive from. 
`Hub.Receive` is a non-blocking call, which takes a message handler func and options. Since Event Hub is just a long
log of messages, you also have to tell it where to start from. By default, a receiver will start from the beginning
of the log, but there are options to help you specify your starting offset.

The `Receive` func returns a handle to the running receiver and an error. If error is returned, the receiver was unable
to start. If error is nil, the receiver is running and can be stopped by calling `Close` on the `Hub` or the handle
returned.

- Receive messages from a partition from the beginning of the log
    ```go
    handle, err := hub.Receive(ctx, partitionID, func(ctx context.Context, event *eventhub.Event) error {
 	    // do stuff
    })
    ```
- Receive from the latest message onward
    ```go
    handle, err := hub.Receive(ctx, partitionID, handler, eventhub.ReceiveWithLatestOffset())
    ```
- Receive from a specified offset
    ```go
    handle, err := hub.Receive(ctx, partitionID, handler, eventhub.ReceiveWithStartingOffset(offset))
    ```

At some point, a receiver process is going to stop. You will likely want it to start back up at the spot that it stopped
processing messages. This is where message offsets can be used to start from where you have left off.

The `Hub` struct can be customized to use an `persist.CheckpointPersister`. By default, a `Hub` uses an in-memory
`CheckpointPersister`, but accepts anything that implements the `perist.CheckpointPersister` interface.

```go
// CheckpointPersister provides persistence for the received offset for a given namespace, hub name, consumer group, partition Id and
// offset so that if a receiver where to be interrupted, it could resume after the last consumed event.
CheckpointPersister interface {
    Write(namespace, name, consumerGroup, partitionID string, checkpoint Checkpoint) error
    Read(namespace, name, consumerGroup, partitionID string) (Checkpoint, error)
}
```

For example, you could use the persist.FilePersister to save your checkpoints to a directory.
```go
persister, err := persist.NewFilePersister(directoryPath)
if err != nil {
	// handle err
}
hub, err := eventhub.NewHubFromEnvironment(eventhub.HubWithOffsetPersistence(persister))
```

## Event Processor Host
The key to scale for Event Hubs is the idea of partitioned consumers. In contrast to the 
[competing consumers pattern](https://docs.microsoft.com/en-us/previous-versions/msp-n-p/dn568101(v=pandp.10)), 
the partitioned consumer pattern enables high scale by removing the contention bottleneck and facilitating end to end 
parallelism.

The Event Processor Host (EPH) is an intelligent consumer agent that simplifies the management of checkpointing, 
leasing, and parallel event readers. EPH is intended to be run across multiple processes and machines while load
balancing message consumers. A message consumer in EPH will take a lease on a partition, begin processing messages and
periodically write a check point to a persistent store. If at any time a new EPH process is added or lost, the remaining
processors will balance the existing leases amongst the set of EPH processes.

The default implementation of partition leasing and check pointing is based on Azure Storage. Below is an example using
EPH to start listening to all of the partitions of an Event Hub and print the messages received.

### Receiving Events

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"
	
	"github.com/Azure/azure-amqp-common-go/v3/conn"
	"github.com/Azure/azure-amqp-common-go/v3/sas"
    "github.com/Azure/azure-event-hubs-go/v3"
    "github.com/Azure/azure-event-hubs-go/v3/eph"
    "github.com/Azure/azure-event-hubs-go/v3/storage"
	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/Azure/go-autorest/autorest/azure"
)

func main() {
	// Azure Storage account information
    storageAccountName := "mystorageaccount"
    storageAccountKey := "Zm9vCg=="
    // Azure Storage container to store leases and checkpoints
    storageContainerName := "ephcontainer"
    
    // Azure Event Hub connection string
    eventHubConnStr := "Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=superSecret1234=;EntityPath=hubName"
    parsed, err := conn.ParsedConnectionFromStr(eventHubConnStr)
    if err != nil {
        // handle error
    }
    
    // create a new Azure Storage Leaser / Checkpointer
    cred, err := azblob.NewSharedKeyCredential(storageAccountName, storageAccountKey)
    if err != nil {
	fmt.Println(err)
	return
    }

    leaserCheckpointer, err := storage.NewStorageLeaserCheckpointer(cred, storageAccountName, storageContainerName, azure.PublicCloud)
    if err != nil {
	fmt.Println(err)
	return
    }
    
    // SAS token provider for Azure Event Hubs
    provider, err := sas.NewTokenProvider(sas.TokenProviderWithKey(parsed.KeyName, parsed.Key))
    if err != nil {
	fmt.Println(err)
	return
    }
    
    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
    defer cancel()
    // create a new EPH processor
    processor, err := eph.New(ctx, parsed.Namespace, parsed.HubName, provider, leaserCheckpointer, leaserCheckpointer)
    if err != nil {
	fmt.Println(err)
	return
    }
    
    // register a message handler -- many can be registered
    handlerID, err := processor.RegisterHandler(ctx, 
	func(c context.Context, e *eventhub.Event) error {
		fmt.Println(string(e.Data))
    		return nil
    })
    if err != nil {
	fmt.Println(err)
	return
    }
    
    fmt.Printf("handler id: %q is running\n", handlerID)

    // unregister a handler to stop that handler from receiving events
    // processor.UnregisterHandler(ctx, handleID)
    
    // start handling messages from all of the partitions balancing across multiple consumers
    err = processor.StartNonBlocking(ctx)
    if err != nil {
        fmt.Println(err)
        return
    }
    
    // Wait for a signal to quit:
    signalChan := make(chan os.Signal, 1)
    signal.Notify(signalChan, os.Interrupt, os.Kill)
    <-signalChan
    
    err = processor.Close(context.Background())
    if err != nil {
        fmt.Println(err)
        return
    }
}
```

## Examples
- [HelloWorld: Producer and Consumer](./_examples/helloworld): an example of sending and receiving messages from an
Event Hub instance.
- [Batch Processing](./_examples/batchprocessing): an example of handling events in batches

# Contributing

This project welcomes contributions and suggestions.  Most contributions require you to agree to a
Contributor License Agreement (CLA) declaring that you have the right to, and actually do, grant us
the rights to use your contribution. For details, visit https://cla.microsoft.com.

When you submit a pull request, a CLA-bot will automatically determine whether you need to provide
a CLA and decorate the PR appropriately (e.g., label, comment). Simply follow the instructions
provided by the bot. You will only need to do this once across all repos using our CLA.

This project has adopted the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/).
For more information see the [Code of Conduct FAQ](https://opensource.microsoft.com/codeofconduct/faq/) or
contact [opencode@microsoft.com](mailto:opencode@microsoft.com) with any additional questions or comments.

See [CONTRIBUTING.md](./.github/CONTRIBUTING.md).

## Running Tests
To setup the integration test environment, ensure the following pre-requisites are in place
- [install WSL](https://docs.microsoft.com/en-us/windows/wsl/install-win10) (if on Windows)
- [install golang](https://golang.org/doc/install) 
- add paths to .profile
  - export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
  - export GOPATH=$HOME/go
- install go dev dependencies
  - run `go get github.com/fzipp/gocyclo`
  - run `go get -u golang.org/x/lint/golint`
- run the following bash commands
  - `sudo apt install jq`
- install gcc
  - on Ubuntu:
    - `sudo apt update`
    - `sudo apt install build-essential`
- [download terraform](https://www.terraform.io/downloads.html) and add to the path
- install [Azure CLI](https://docs.microsoft.com/en-us/cli/azure/install-azure-cli-apt?view=azure-cli-latest)
- run `az login`

To run all tests run `make test`

To cleanup dev tools in `go.mod` and `go.sum` prior to check-in run `make tidy` or `go mode tidy`

# License

MIT, see [LICENSE](./LICENSE).
