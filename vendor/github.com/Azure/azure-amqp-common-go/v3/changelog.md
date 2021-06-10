# Change Log

## `head`

## `v2.1.1`
- bump amqp to v0.12.1
- bump azure sdk for go to v32.5.0
- bump go-autorest

## `v2.1.0`
- add session filters for RPC links
- bump amqp to v0.11.2
- add more logging in RPC operations

## `v2.0.0`
- [**breaking change** remove persist and move into the Event Hubs package](https://github.com/Azure/azure-event-hubs-go/pull/112)
- **breaking change** remove log package in favor of https://github.com/devigned/tab

## `v1.1.4`
- allow status description on RPC calls to be empty without returning an error https://github.com/Azure/azure-event-hubs-go/issues/88

## `v1.1.3`
- adding automatic server-timeout field for `rpc` package. It gleans the appropriate value from the context passed to it

## `v1.1.2`
- adopting go modules 

## `v1.1.1`
- broadening accepted versions of pack.ag/amqp

## `v1.1.0`

- adding the ability to reuse an AMQP session while making RPCs
- bug fixes

## `v1.0.3`
- updating dependencies, adding new 'go-autorest' constraint

## `v1.0.2`
- adding resiliency against malformed "status-code" and "status-description" properties in rpc responses

## `v1.0.1`
- bump version constant

## `v1.0.0`
- moved to opencensus from opentracing
- committing to backward compatibility

## `v0.7.0`
- update AMQP dependency to 0.7.0

## `v0.6.0`
- **Breaking Change** change the parse connection signature and make it more strict
- fix errors imports

## `v0.5.0`
- **Breaking Change** lock dependency to AMQP

## `v0.4.0`
- **Breaking Change** remove namespace from SAS provider and return struct rather than interface 

## `v0.3.2`
- Return error on retry. Was returning nil if not retryable.

## `v0.3.1`
- Fix missing defer on spans

## `v0.3.0`
- add opentracing support
- upgrade amqp to pull in the changes where close accepts context (breaking change)

## `v0.2.4`
- connection string keys are case insensitive 

## `v0.2.3`
- handle remove trailing slash from host

## `v0.2.2`
- handle connection string values which contain `=`

## `v0.2.1`
- parse connection strings using key / values rather than regex

## `v0.2.0`
- add file checkpoint persister

## `v0.1.0`
- initial release