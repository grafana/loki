# v1.52.4 (2025-11-04)

* **Dependency Update**: Updated to the latest SDK module versions
* **Dependency Update**: Upgrade to smithy-go v1.23.2 which should convey some passive reduction of overall allocations, especially when not using the metrics system.

# v1.52.3 (2025-10-30)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.52.2 (2025-10-23)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.52.1 (2025-10-22)

* No change notes available for this release.

# v1.52.0 (2025-10-21)

* **Feature**: Add AccountID based endpoint metric to endpoint rules.

# v1.51.1 (2025-10-16)

* **Dependency Update**: Bump minimum Go version to 1.23.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.51.0 (2025-10-02)

* **Feature**: Add support for dual-stack account endpoint generation

# v1.50.5 (2025-09-26)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.50.4 (2025-09-23)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.50.3 (2025-09-10)

* No change notes available for this release.

# v1.50.2 (2025-09-08)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.50.1 (2025-08-29)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.50.0 (2025-08-28)

* **Feature**: Remove incorrect endpoint tests

# v1.49.2 (2025-08-27)

* **Dependency Update**: Update to smithy-go v1.23.0.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.49.1 (2025-08-21)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.49.0 (2025-08-20)

* **Feature**: Remove incorrect endpoint tests
* **Bug Fix**: Remove unused deserialization code.

# v1.48.0 (2025-08-14)

* **Feature**: This release 1/ Adds support for throttled keys mode for CloudWatch Contributor Insights, 2/ Adds throttling reasons to exceptions across dataplane APIs. 3/ Explicitly models ThrottlingException as a class in statically typed languages. Refer to the launch day blog post for more details.

# v1.47.0 (2025-08-11)

* **Feature**: Add support for configuring per-service Options via callback on global config.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.46.0 (2025-08-04)

* **Feature**: Support configurable auth scheme preferences in service clients via AWS_AUTH_SCHEME_PREFERENCE in the environment, auth_scheme_preference in the config file, and through in-code settings on LoadDefaultConfig and client constructor methods.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.45.1 (2025-07-30)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.45.0 (2025-07-28)

* **Feature**: Add support for HTTP interceptors.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.44.1 (2025-07-19)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.44.0 (2025-06-30)

* **Feature**: This change adds support for witnesses in global tables. It also adds a new table status, REPLICATION_NOT_AUTHORIZED. This status will indicate scenarios where global replicas table can't be utilized for data plane operations.

# v1.43.4 (2025-06-17)

* **Dependency Update**: Update to smithy-go v1.22.4.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.43.3 (2025-06-10)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.43.2 (2025-06-06)

* No change notes available for this release.

# v1.43.1 (2025-04-28)

* **Documentation**: Doc only update for GSI descriptions.

# v1.43.0 (2025-04-24)

* **Feature**: Add support for ARN-sourced account endpoint generation for TransactWriteItems. This will generate account endpoints for DynamoDB TransactWriteItems requests using ARN-sourced account ID when available.

# v1.42.4 (2025-04-11)

* **Documentation**: Doc only update for API descriptions.

# v1.42.3 (2025-04-10)

* No change notes available for this release.

# v1.42.2 (2025-04-09)

* **Documentation**: Documentation update for secondary indexes and Create_Table.

# v1.42.1 (2025-04-03)

* No change notes available for this release.

# v1.42.0 (2025-03-13)

* **Feature**: Generate account endpoints for DynamoDB requests using ARN-sourced account ID when available

# v1.41.1 (2025-03-04.2)

* **Bug Fix**: Add assurance test for operation order.

# v1.41.0 (2025-02-27)

* **Feature**: Track credential providers via User-Agent Feature ids
* **Dependency Update**: Updated to the latest SDK module versions

# v1.40.2 (2025-02-18)

* **Bug Fix**: Add missing AccountIDEndpointMode binding to endpoint resolution.
* **Bug Fix**: Bump go version to 1.22
* **Dependency Update**: Updated to the latest SDK module versions

# v1.40.1 (2025-02-11)

* No change notes available for this release.

# v1.40.0 (2025-02-05)

* **Feature**: Track AccountID endpoint mode in user-agent.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.39.9 (2025-02-04)

* No change notes available for this release.

# v1.39.8 (2025-01-31)

* **Dependency Update**: Switch to code-generated waiter matchers, removing the dependency on go-jmespath.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.39.7 (2025-01-30)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.39.6 (2025-01-24)

* **Dependency Update**: Updated to the latest SDK module versions
* **Dependency Update**: Upgrade to smithy-go v1.22.2.

# v1.39.5 (2025-01-17)

* **Bug Fix**: Fix bug where credentials weren't refreshed during retry loop.

# v1.39.4 (2025-01-15)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.39.3 (2025-01-14)

* **Bug Fix**: Fix issue where waiters were not failing on unmatched errors as they should. This may have breaking behavioral changes for users in fringe cases. See [this announcement](https://github.com/aws/aws-sdk-go-v2/discussions/2954) for more information.

# v1.39.2 (2025-01-09)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.39.1 (2025-01-08)

* No change notes available for this release.

# v1.39.0 (2025-01-07)

* **Feature**: This release makes Amazon DynamoDB point-in-time-recovery (PITR) to be configurable. You can set PITR recovery period for each table individually to between 1 and 35 days.

# v1.38.1 (2024-12-19)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.38.0 (2024-12-03.2)

* **Feature**: This change adds support for global tables with multi-Region strong consistency (in preview). The UpdateTable API now supports a new attribute MultiRegionConsistency to set consistency when creating global tables. The DescribeTable output now optionally includes the MultiRegionConsistency attribute.

# v1.37.2 (2024-12-02)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.37.1 (2024-11-18)

* **Dependency Update**: Update to smithy-go v1.22.1.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.37.0 (2024-11-13)

* **Feature**: This release includes supports the new WarmThroughput feature for DynamoDB. You can now provide an optional WarmThroughput attribute for CreateTable or UpdateTable APIs to pre-warm your table or global secondary index. You can also use DescribeTable to see the latest WarmThroughput value.

# v1.36.5 (2024-11-07)

* **Bug Fix**: Adds case-insensitive handling of error message fields in service responses

# v1.36.4 (2024-11-06)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.36.3 (2024-10-28)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.36.2 (2024-10-08)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.36.1 (2024-10-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.36.0 (2024-10-04)

* **Feature**: Add support for HTTP client metrics.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.35.4 (2024-10-03)

* No change notes available for this release.

# v1.35.3 (2024-09-27)

* No change notes available for this release.

# v1.35.2 (2024-09-25)

* No change notes available for this release.

# v1.35.1 (2024-09-23)

* No change notes available for this release.

# v1.35.0 (2024-09-20)

* **Feature**: Add tracing and metrics support to service clients.
* **Feature**: Generate and use AWS-account-based endpoints for DynamoDB requests when the account ID is available. The new endpoint URL pattern will be https://<account-id>.ddb.<region>.amazonaws.com. See the documentation for details: https://docs.aws.amazon.com/sdkref/latest/guide/feature-account-endpoints.html.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.34.10 (2024-09-17)

* **Bug Fix**: **BREAKFIX**: Only generate AccountIDEndpointMode config for services that use it. This is a compiler break, but removes no actual functionality, as no services currently use the account ID in endpoint resolution.

# v1.34.9 (2024-09-09)

* **Documentation**: Doc-only update for DynamoDB. Added information about async behavior for TagResource and UntagResource APIs and updated the description of ResourceInUseException.

# v1.34.8 (2024-09-04)

* No change notes available for this release.

# v1.34.7 (2024-09-03)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.34.6 (2024-08-22)

* No change notes available for this release.

# v1.34.5 (2024-08-15)

* **Dependency Update**: Bump minimum Go version to 1.21.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.34.4 (2024-07-24)

* **Documentation**: DynamoDB doc only update for July

# v1.34.3 (2024-07-10.2)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.34.2 (2024-07-10)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.34.1 (2024-06-28)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.34.0 (2024-06-26)

* **Feature**: Support list-of-string endpoint parameter.

# v1.33.2 (2024-06-20)

* **Documentation**: Doc-only update for DynamoDB. Fixed Important note in 6 Global table APIs - CreateGlobalTable, DescribeGlobalTable, DescribeGlobalTableSettings, ListGlobalTables, UpdateGlobalTable, and UpdateGlobalTableSettings.

# v1.33.1 (2024-06-19)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.33.0 (2024-06-18)

* **Feature**: Track usage of various AWS SDK features in user-agent string.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.32.9 (2024-06-17)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.32.8 (2024-06-07)

* **Bug Fix**: Add clock skew correction on all service clients
* **Dependency Update**: Updated to the latest SDK module versions

# v1.32.7 (2024-06-03)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.32.6 (2024-05-28)

* **Documentation**: Doc-only update for DynamoDB. Specified the IAM actions needed to authorize a user to create a table with a resource-based policy.

# v1.32.5 (2024-05-24)

* **Documentation**: Documentation only updates for DynamoDB.

# v1.32.4 (2024-05-23)

* No change notes available for this release.

# v1.32.3 (2024-05-16)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.32.2 (2024-05-15)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.32.1 (2024-05-08)

* **Bug Fix**: GoDoc improvement

# v1.32.0 (2024-05-02)

* **Feature**: This release adds support to specify an optional, maximum OnDemandThroughput for DynamoDB tables and global secondary indexes in the CreateTable or UpdateTable APIs. You can also override the OnDemandThroughput settings by calling the ImportTable, RestoreFromPointInTime, or RestoreFromBackup APIs.

# v1.31.1 (2024-03-29)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.31.0 (2024-03-20)

* **Feature**: This release introduces 3 new APIs ('GetResourcePolicy', 'PutResourcePolicy' and 'DeleteResourcePolicy') and modifies the existing 'CreateTable' API for the resource-based policy support. It also modifies several APIs to accept a 'TableArn' for the 'TableName' parameter.

# v1.30.5 (2024-03-18)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.30.4 (2024-03-07)

* **Bug Fix**: Remove dependency on go-cmp.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.30.3 (2024-03-06)

* **Documentation**: Doc only updates for DynamoDB documentation

# v1.30.2 (2024-03-04)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.30.1 (2024-02-23)

* **Bug Fix**: Move all common, SDK-side middleware stack ops into the service client module to prevent cross-module compatibility issues in the future.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.30.0 (2024-02-22)

* **Feature**: Add middleware stack snapshot tests.

# v1.29.2 (2024-02-21)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.29.1 (2024-02-20)

* **Bug Fix**: When sourcing values for a service's `EndpointParameters`, the lack of a configured region (i.e. `options.Region == ""`) will now translate to a `nil` value for `EndpointParameters.Region` instead of a pointer to the empty string `""`. This will result in a much more explicit error when calling an operation instead of an obscure hostname lookup failure.
* **Documentation**: Publishing quick fix for doc only update.

# v1.29.0 (2024-02-16)

* **Feature**: Add new ClientOptions field to waiter config which allows you to extend the config for operation calls made by waiters.

# v1.28.1 (2024-02-15)

* **Bug Fix**: Correct failure to determine the error type in awsJson services that could occur when errors were modeled with a non-string `code` field.

# v1.28.0 (2024-02-13)

* **Feature**: Bump minimum Go version to 1.20 per our language support policy.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.27.1 (2024-02-02)

* **Documentation**: Any number of users can execute up to 50 concurrent restores (any type of restore) in a given account.

# v1.27.0 (2024-01-19)

* **Feature**: This release adds support for including ApproximateCreationDateTimePrecision configurations in EnableKinesisStreamingDestination API, adds the same as an optional field in the response of DescribeKinesisStreamingDestination, and adds support for a new UpdateKinesisStreamingDestination API.

# v1.26.9 (2024-01-17)

* **Documentation**: Updating note for enabling streams for UpdateTable.

# v1.26.8 (2024-01-04)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.26.7 (2023-12-20)

* No change notes available for this release.

# v1.26.6 (2023-12-08)

* **Bug Fix**: Reinstate presence of default Retryer in functional options, but still respect max attempts set therein.

# v1.26.5 (2023-12-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.26.4 (2023-12-06)

* **Bug Fix**: Restore pre-refactor auth behavior where all operations could technically be performed anonymously.

# v1.26.3 (2023-12-01)

* **Bug Fix**: Correct wrapping of errors in authentication workflow.
* **Bug Fix**: Correctly recognize cache-wrapped instances of AnonymousCredentials at client construction.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.26.2 (2023-11-30.2)

* **Bug Fix**: Respect caller region overrides in endpoint discovery.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.26.1 (2023-11-30)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.26.0 (2023-11-29)

* **Feature**: Expose Options() accessor on service clients.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.25.5 (2023-11-28.2)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.25.4 (2023-11-28)

* **Bug Fix**: Respect setting RetryMaxAttempts in functional options at client construction.

# v1.25.3 (2023-11-20)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.25.2 (2023-11-15)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.25.1 (2023-11-09)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.25.0 (2023-11-01)

* **Feature**: Adds support for configured endpoints via environment variables and the AWS shared configuration file.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.24.0 (2023-10-31)

* **Feature**: **BREAKING CHANGE**: Bump minimum go version to 1.19 per the revised [go version support policy](https://aws.amazon.com/blogs/developer/aws-sdk-for-go-aligns-with-go-release-policy-on-supported-runtimes/).
* **Dependency Update**: Updated to the latest SDK module versions

# v1.23.0 (2023-10-18)

* **Feature**: Add handwritten paginators that were present in some services in the v1 SDK.
* **Documentation**: Updating descriptions for several APIs.

# v1.22.2 (2023-10-12)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.22.1 (2023-10-06)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.22.0 (2023-09-26)

* **Feature**: Amazon DynamoDB now supports Incremental Export as an enhancement to the existing Export Table

# v1.21.5 (2023-08-21)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.21.4 (2023-08-18)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.21.3 (2023-08-17)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.21.2 (2023-08-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.21.1 (2023-08-01)

* No change notes available for this release.

# v1.21.0 (2023-07-31)

* **Feature**: Adds support for smithy-modeled endpoint resolution. A new rules-based endpoint resolution will be added to the SDK which will supercede and deprecate existing endpoint resolution. Specifically, EndpointResolver will be deprecated while BaseEndpoint and EndpointResolverV2 will take its place. For more information, please see the Endpoints section in our Developer Guide.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.20.3 (2023-07-28)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.20.2 (2023-07-25)

* **Documentation**: Documentation updates for DynamoDB

# v1.20.1 (2023-07-13)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.20.0 (2023-06-29)

* **Feature**: This release adds ReturnValuesOnConditionCheckFailure parameter to PutItem, UpdateItem, DeleteItem, ExecuteStatement, BatchExecuteStatement and ExecuteTransaction APIs. When set to ALL_OLD,  API returns a copy of the item as it was when a conditional write failed

# v1.19.11 (2023-06-21)

* **Documentation**: Documentation updates for DynamoDB

# v1.19.10 (2023-06-15)

* No change notes available for this release.

# v1.19.9 (2023-06-13)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.19.8 (2023-06-12)

* **Documentation**: Documentation updates for DynamoDB

# v1.19.7 (2023-05-04)

* No change notes available for this release.

# v1.19.6 (2023-04-24)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.19.5 (2023-04-17)

* **Documentation**: Documentation updates for DynamoDB API

# v1.19.4 (2023-04-10)

* No change notes available for this release.

# v1.19.3 (2023-04-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.19.2 (2023-03-21)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.19.1 (2023-03-10)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.19.0 (2023-03-08)

* **Feature**: Adds deletion protection support to DynamoDB tables. Tables with deletion protection enabled cannot be deleted. Deletion protection is disabled by default, can be enabled via the CreateTable or UpdateTable APIs, and is visible in TableDescription. This setting is not replicated for Global Tables.

# v1.18.6 (2023-03-03)

* **Documentation**: Documentation updates for DynamoDB.

# v1.18.5 (2023-02-22)

* **Bug Fix**: Prevent nil pointer dereference when retrieving error codes.

# v1.18.4 (2023-02-20)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.3 (2023-02-15)

* **Announcement**: When receiving an error response in restJson-based services, an incorrect error type may have been returned based on the content of the response. This has been fixed via PR #2012 tracked in issue #1910.
* **Bug Fix**: Correct error type parsing for restJson services.

# v1.18.2 (2023-02-03)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.1 (2023-01-23)

* No change notes available for this release.

# v1.18.0 (2023-01-05)

* **Feature**: Add `ErrorCodeOverride` field to all error structs (aws/smithy-go#401).

# v1.17.9 (2022-12-15)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.17.8 (2022-12-02)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.17.7 (2022-11-22)

* No change notes available for this release.

# v1.17.6 (2022-11-18)

* **Documentation**: Updated minor fixes for DynamoDB documentation.

# v1.17.5 (2022-11-16)

* No change notes available for this release.

# v1.17.4 (2022-11-10)

* No change notes available for this release.

# v1.17.3 (2022-10-24)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.17.2 (2022-10-21)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.17.1 (2022-09-20)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.17.0 (2022-09-15)

* **Feature**: Increased DynamoDB transaction limit from 25 to 100.

# v1.16.5 (2022-09-14)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.16.4 (2022-09-02)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.16.3 (2022-08-31)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.16.2 (2022-08-30)

* No change notes available for this release.

# v1.16.1 (2022-08-29)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.16.0 (2022-08-18)

* **Feature**: This release adds support for importing data from S3 into a new DynamoDB table

# v1.15.13 (2022-08-11)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.15.12 (2022-08-09)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.15.11 (2022-08-08)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.15.10 (2022-08-01)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.15.9 (2022-07-05)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.15.8 (2022-06-29)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.15.7 (2022-06-17)

* **Documentation**: Doc only update for DynamoDB service

# v1.15.6 (2022-06-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.15.5 (2022-05-17)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.15.4 (2022-04-25)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.15.3 (2022-03-30)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.15.2 (2022-03-24)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.15.1 (2022-03-23)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.15.0 (2022-03-08)

* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.14.0 (2022-02-24)

* **Feature**: API client updated
* **Feature**: Adds RetryMaxAttempts and RetryMod to API client Options. This allows the API clients' default Retryer to be configured from the shared configuration files or environment variables. Adding a new Retry mode of `Adaptive`. `Adaptive` retry mode is an experimental mode, adding client rate limiting when throttles reponses are received from an API. See [retry.AdaptiveMode](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/aws/retry#AdaptiveMode) for more details, and configuration options.
* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.13.0 (2022-01-14)

* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.12.0 (2022-01-07)

* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.11.0 (2021-12-21)

* **Feature**: API Paginators now support specifying the initial starting token, and support stopping on empty string tokens.
* **Feature**: Updated to latest service endpoints

# v1.10.0 (2021-12-02)

* **Feature**: API client updated
* **Bug Fix**: Fixes a bug that prevented aws.EndpointResolverWithOptions from being used by the service client. ([#1514](https://github.com/aws/aws-sdk-go-v2/pull/1514))
* **Dependency Update**: Updated to the latest SDK module versions

# v1.9.0 (2021-11-30)

* **Feature**: API client updated
* **Dependency Update**: Updated to the latest SDK module versions

# v1.8.1 (2021-11-19)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.8.0 (2021-11-12)

* **Feature**: Service clients now support custom endpoints that have an initial URI path defined.
* **Feature**: Waiters now have a `WaitForOutput` method, which can be used to retrieve the output of the successful wait operation. Thank you to [Andrew Haines](https://github.com/haines) for contributing this feature.
* **Documentation**: Updated service to latest API model.

# v1.7.0 (2021-11-06)

* **Feature**: The SDK now supports configuration of FIPS and DualStack endpoints using environment variables, shared configuration, or programmatically.
* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.6.0 (2021-10-21)

* **Feature**: API client updated
* **Feature**: Updated  to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.5.2 (2021-10-11)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.5.1 (2021-09-17)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.5.0 (2021-08-27)

* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.4.3 (2021-08-19)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.4.2 (2021-08-04)

* **Dependency Update**: Updated `github.com/aws/smithy-go` to latest version.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.4.1 (2021-07-15)

* **Dependency Update**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.4.0 (2021-06-25)

* **Feature**: Adds support for endpoint discovery.
* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.3.1 (2021-05-20)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.3.0 (2021-05-14)

* **Feature**: Constant has been added to modules to enable runtime version inspection for reporting.
* **Dependency Update**: Updated to the latest SDK module versions

