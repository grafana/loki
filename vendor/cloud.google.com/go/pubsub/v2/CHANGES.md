# Changes

## [2.3.0](https://github.com/googleapis/google-cloud-go/releases/tag/pubsub%2Fv2.3.0) (2025-10-22)

### Features

* Add AwsKinesisFailureReason.ApiViolationReason 
* Add tags to Subscription, Topic, and CreateSnapshotRequest messages for use in CreateSubscription, CreateTopic, and CreateSnapshot requests respectively 
* Annotate some resource fields with their corresponding API types 

### Documentation

* A comment for field `received_messages` in message `.google.pubsub.v1.StreamingPullResponse` is changed 

## [2.2.1](https://github.com/googleapis/google-cloud-go/compare/pubsub/v2/v2.2.0...pubsub/v2/v2.2.1) (2025-10-14)


### Bug Fixes

* **pubsub/v2:** Avoid Receive hang on context cancellation ([#13114](https://github.com/googleapis/google-cloud-go/issues/13114)) ([e7e169d](https://github.com/googleapis/google-cloud-go/commit/e7e169d1c1e48ad0fb78bcfe23d73f2de76d1f01))
* **pubsub/v2:** Upgrade gRPC service registration func ([8fffca2](https://github.com/googleapis/google-cloud-go/commit/8fffca2819fa3dc858c213aa0c503e0df331b084))

## [2.2.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v2/v2.1.0...pubsub/v2/v2.2.0) (2025-10-03)


### Features

* **pubsub/v2:** Support the protocol version in StreamingPullRequest ([#12985](https://github.com/googleapis/google-cloud-go/issues/12985)) ([4e8c9d5](https://github.com/googleapis/google-cloud-go/commit/4e8c9d50a07d209417d4a5807ab1990160a4fd0b))


### Bug Fixes

* **pubsub/v2:** Respect ShutdownBehavior when handling timeout ([#13021](https://github.com/googleapis/google-cloud-go/issues/13021)) ([0135d93](https://github.com/googleapis/google-cloud-go/commit/0135d9305581444e1ddcdd8f4fe63e4c588b575f))

## [2.1.0](https://github.com/googleapis/google-cloud-go/compare/pubsub/v2/v2.0.1...pubsub/v2/v2.1.0) (2025-09-25)


### Features

* **pubsub/v2:** Add subscriber shutdown options ([#12829](https://github.com/googleapis/google-cloud-go/issues/12829)) ([14c3887](https://github.com/googleapis/google-cloud-go/commit/14c3887819c7bfdf3de661ec807fa82b6bb3183e))

## [2.0.1](https://github.com/googleapis/google-cloud-go/compare/pubsub/v2/v2.0.0...pubsub/v2/v2.0.1) (2025-09-03)


### Bug Fixes

* **pubsub/v2:** Update flowcontrol metrics even when disabled ([#12590](https://github.com/googleapis/google-cloud-go/issues/12590)) ([c153495](https://github.com/googleapis/google-cloud-go/commit/c1534952c4a6c3a52dd9e3aab295d27d4107016c))


### Documentation

* **pubsub/v2:** Move wiki to package doc ([#12605](https://github.com/googleapis/google-cloud-go/issues/12605)) ([3de795e](https://github.com/googleapis/google-cloud-go/commit/3de795ecaf1782df76d9ac49499988369601d334))

## 2.0.0 (2025-07-16)


### Features

* **pubsub/v2:** Add MessageTransformationFailureReason to IngestionFailureEvent ([208745b](https://github.com/googleapis/google-cloud-go/commit/208745bbc1f4fc9122ec71d6cf42f512ae570d13))
* **pubsub/v2:** Add new v2 library ([#12218](https://github.com/googleapis/google-cloud-go/issues/12218)) ([c798f62](https://github.com/googleapis/google-cloud-go/commit/c798f62f908140686b8e2a365cccf9608fb5ab95))
* **pubsub/v2:** Add SchemaViolationReason to IngestionFailureEvent ([d8ae687](https://github.com/googleapis/google-cloud-go/commit/d8ae6874a54b48fce49968664f14db63c055c6e2))
* **pubsub/v2:** Generate renamed go pubsub admin clients ([a95a0bf](https://github.com/googleapis/google-cloud-go/commit/a95a0bf4172b8a227955a0353fd9c845f4502411))
* **pubsub/v2:** Release 2.0.0 ([#12568](https://github.com/googleapis/google-cloud-go/issues/12568)) ([704efce](https://github.com/googleapis/google-cloud-go/commit/704efce43ffd2e81e9fe8e19f7573913b86840e8))


### Documentation

* **pubsub/v2:** Document that the `acknowledge_confirmation` and `modify_ack_deadline_confirmation` fields in message `.google.pubsub.v1.StreamingPullResponse` are not guaranteed to be populated ([208745b](https://github.com/googleapis/google-cloud-go/commit/208745bbc1f4fc9122ec71d6cf42f512ae570d13))
* **pubsub/v2:** Standardize spelling of "acknowledgment" in Pub/Sub protos ([d8ae687](https://github.com/googleapis/google-cloud-go/commit/d8ae6874a54b48fce49968664f14db63c055c6e2))
* **pubsub/v2:** Update v2 package docs with migration guide ([#12564](https://github.com/googleapis/google-cloud-go/issues/12564)) ([5ef6068](https://github.com/googleapis/google-cloud-go/commit/5ef606838a84f1c56225fc4e33f4ee394eb34725))

## Changes
