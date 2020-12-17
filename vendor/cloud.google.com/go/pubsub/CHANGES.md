# Changes

## v1.3.1

- Fix bug with removing dead letter policy from a subscription
- Set default value of MaxExtensionPeriod to 0, which is functionally equivalent

## v1.3.0

- Update cloud.google.com/go to v0.54.0

## v1.2.0

- Add support for upcoming dead letter topics feature
- Expose Subscription.ReceiveSettings.MaxExtensionPeriod setting
- Standardize default settings with other client libraries
  - Increase publish delay threshold from 1ms to 10ms
  - Increase subscription MaxExtension from 10m to 60m
- Always send keepalive/heartbeat ping on StreamingPull streams to minimize
  stream reopen requests

## v1.1.0

- Limit default grpc connections to 4.
- Fix issues with OpenCensus metric for pull count not including synchronous pull messages.
- Fix issue with publish bundle size calculations.
- Add ClearMessages method to pstest server.

## v1.0.1

Small fix to a package name.

## v1.0.0

This is the first tag to carve out pubsub as its own module. See:
https://github.com/golang/go/wiki/Modules#is-it-possible-to-add-a-module-to-a-multi-module-repository.
