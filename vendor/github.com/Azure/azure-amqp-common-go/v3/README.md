# Azure AMQP Common
[![Go Report Card](https://goreportcard.com/badge/github.com/Azure/azure-amqp-common-go)](https://goreportcard.com/report/github.com/Azure/azure-amqp-common-go)
[![godoc](https://godoc.org/github.com/Azure/azure-amqp-common-go?status.svg)](https://godoc.org/github.com/Azure/azure-amqp-common-go)
[![Build Status](https://travis-ci.org/Azure/azure-amqp-common-go.svg?branch=master)](https://travis-ci.org/Azure/azure-amqp-common-go)

This project contains reusable components for AMQP based services like Event Hub and Service Bus. You will find 
abstractions over authentication, claims-based security, connection string parsing and RPC for AMQP.

If you are looking for the Azure Event Hub library for go, you can find it [here](https://aka.ms/azure-event-hubs-go).

If you are looking for the Azure Service Bus library for go, you can find it [here](https://aka.ms/azure-service-bus-go).

## Install with Go modules
If you want to use stable versions of the library, please use Go modules.

**NOTE**: versions prior to 3.0.0 depend on pack.ag/amqp which is no longer maintained.
Any new code should not use versions prior to 3.0.0.

### Using go get targeting version 3.x.x
``` bash
go get -u github.com/Azure/azure-amqp-common-go/v3
```

### Using go get targeting version 2.x.x
``` bash
go get -u github.com/Azure/azure-amqp-common-go/v2
```

### Using go get targeting version 1.x.x
``` bash
go get -u github.com/Azure/azure-amqp-common-go
```

## Contributing

This project welcomes contributions and suggestions.  Most contributions require you to agree to a
Contributor License Agreement (CLA) declaring that you have the right to, and actually do, grant us
the rights to use your contribution. For details, visit https://cla.microsoft.com.

When you submit a pull request, a CLA-bot will automatically determine whether you need to provide
a CLA and decorate the PR appropriately (e.g., label, comment). Simply follow the instructions
provided by the bot. You will only need to do this once across all repos using our CLA.

This project has adopted the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/).
For more information see the [Code of Conduct FAQ](https://opensource.microsoft.com/codeofconduct/faq/) or
contact [opencode@microsoft.com](mailto:opencode@microsoft.com) with any additional questions or comments.

## License

MIT, see [LICENSE](./LICENSE).

## Contribute

See [CONTRIBUTING.md](.github/CONTRIBUTING.md).
