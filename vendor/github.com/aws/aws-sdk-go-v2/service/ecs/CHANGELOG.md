# v1.70.0 (2025-12-18)

* **Feature**: Adding support for Event Windows via a new ECS account setting "fargateEventWindows". When enabled, ECS Fargate will use the configured event window for patching tasks. Introducing "CapacityOptionType" for CreateCapacityProvider API, allowing support for Spot capacity for ECS Managed Instances.

# v1.69.5 (2025-12-09)

* No change notes available for this release.

# v1.69.4 (2025-12-08)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.69.3 (2025-12-05)

* **Documentation**: Updating stop-task API to encapsulate containers with custom stop signal

# v1.69.2 (2025-12-02)

* **Dependency Update**: Updated to the latest SDK module versions
* **Dependency Update**: Upgrade to smithy-go v1.24.0. Notably this version of the library reduces the allocation footprint of the middleware system. We observe a ~10% reduction in allocations per SDK call with this change.

# v1.69.1 (2025-11-25)

* **Bug Fix**: Add error check for endpoint param binding during auth scheme resolution to fix panic reported in #3234

# v1.69.0 (2025-11-20)

* **Feature**: Launching Amazon ECS Express Mode - a new feature that enables developers to quickly launch highly available, scalable containerized applications with a single command.

# v1.68.1 (2025-11-19.2)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.68.0 (2025-11-19)

* **Feature**: Added support for Amazon ECS Managed Instances infrastructure optimization configuration.

# v1.67.4 (2025-11-12)

* **Bug Fix**: Further reduce allocation overhead when the metrics system isn't in-use.
* **Bug Fix**: Reduce allocation overhead when the client doesn't have any HTTP interceptors configured.
* **Bug Fix**: Remove blank trace spans towards the beginning of the request that added no additional information. This conveys a slight reduction in overall allocations.

# v1.67.3 (2025-11-11)

* **Bug Fix**: Return validation error if input region is not a valid host label.

# v1.67.2 (2025-11-04)

* **Dependency Update**: Updated to the latest SDK module versions
* **Dependency Update**: Upgrade to smithy-go v1.23.2 which should convey some passive reduction of overall allocations, especially when not using the metrics system.

# v1.67.1 (2025-11-03)

* **Documentation**: Documentation-only update for LINEAR and CANARY deployment strategies.

# v1.67.0 (2025-10-30)

* **Feature**: Amazon ECS Service Connect now supports Envoy access logs, providing deeper observability into request-level traffic patterns and service interactions.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.66.0 (2025-10-28)

* **Feature**: Amazon ECS supports native linear and canary service deployments, allowing you to shift traffic in increments for more control.

# v1.65.4 (2025-10-23)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.65.3 (2025-10-22)

* No change notes available for this release.

# v1.65.2 (2025-10-16)

* **Dependency Update**: Bump minimum Go version to 1.23.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.65.1 (2025-10-01)

* **Documentation**: This is a documentation only Amazon ECS release that adds additional information for health checks.

# v1.65.0 (2025-09-30)

* **Feature**: This release adds support for Managed Instances on Amazon ECS.

# v1.64.2 (2025-09-26)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.64.1 (2025-09-23)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.64.0 (2025-09-11)

* **Feature**: This release supports hook details for Amazon ECS lifecycle hooks.

# v1.63.7 (2025-09-10)

* No change notes available for this release.

# v1.63.6 (2025-09-08)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.63.5 (2025-09-05)

* **Documentation**: This is a documentation only release that adds additional information for Amazon ECS Availability Zone rebalancing.

# v1.63.4 (2025-08-29)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.63.3 (2025-08-27)

* **Dependency Update**: Update to smithy-go v1.23.0.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.63.2 (2025-08-21)

* **Documentation**: This is a documentation only release that adds additional information for the update-service request parameters.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.63.1 (2025-08-20)

* **Bug Fix**: Remove unused deserialization code.

# v1.63.0 (2025-08-11)

* **Feature**: Add support for configuring per-service Options via callback on global config.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.62.0 (2025-08-04)

* **Feature**: Support configurable auth scheme preferences in service clients via AWS_AUTH_SCHEME_PREFERENCE in the environment, auth_scheme_preference in the config file, and through in-code settings on LoadDefaultConfig and client constructor methods.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.61.1 (2025-07-30)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.61.0 (2025-07-28)

* **Feature**: Add support for HTTP interceptors.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.60.1 (2025-07-19)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.60.0 (2025-07-16)

* **Feature**: This release removes hookDetails for the Amazon ECS native blue/green deployments.

# v1.59.0 (2025-07-15)

* **Feature**: Amazon ECS supports native blue/green deployments, allowing you to validate new service revisions before directing production traffic to them.

# v1.58.1 (2025-06-25)

* **Documentation**: Updates for change to Amazon ECS default log driver mode from blocking to non-blocking

# v1.58.0 (2025-06-20)

* **Feature**: Add ECS support for Windows Server 2025

# v1.57.6 (2025-06-17)

* **Dependency Update**: Update to smithy-go v1.22.4.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.57.5 (2025-06-12)

* **Documentation**: This Amazon ECS  release supports updating the capacityProviderStrategy parameter in update-service.

# v1.57.4 (2025-06-10)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.57.3 (2025-06-06)

* No change notes available for this release.

# v1.57.2 (2025-06-02)

* **Documentation**: Updates Amazon ECS documentation to include note for upcoming default log driver mode change.

# v1.57.1 (2025-05-16)

* **Documentation**: This is an Amazon ECs documentation only release to support the change of the container exit "reason" field from 255 characters to 1024 characters.

# v1.57.0 (2025-05-13)

* **Feature**: This release extends functionality for Amazon EBS volumes attached to Amazon ECS tasks by adding support for the new EBS volumeInitializationRate parameter in ECS RunTask/StartTask/CreateService/UpdateService APIs.

# v1.56.3 (2025-05-05)

* **Documentation**: Add support to roll back an In_Progress ECS Service Deployment

# v1.56.2 (2025-04-25)

* **Documentation**: Documentation only release for Amazon ECS.

# v1.56.1 (2025-04-24)

* **Documentation**: Documentation only release for Amazon ECS

# v1.56.0 (2025-04-23)

* **Feature**: Add support to roll back an In_Progress ECS Service Deployment

# v1.55.0 (2025-04-17)

* **Feature**: Adds a new AccountSetting - defaultLogDriverMode for ECS.

# v1.54.6 (2025-04-10)

* No change notes available for this release.

# v1.54.5 (2025-04-03)

* No change notes available for this release.

# v1.54.4 (2025-04-02)

* **Documentation**: This is an Amazon ECS documentation only update to address various tickets.

# v1.54.3 (2025-03-28)

* **Documentation**: This is an Amazon ECS documentation only release that addresses tickets.

# v1.54.2 (2025-03-11)

* **Documentation**: This is a documentation only update for Amazon ECS to address various tickets.

# v1.54.1 (2025-03-04.2)

* **Bug Fix**: Add assurance test for operation order.

# v1.54.0 (2025-02-27)

* **Feature**: Track credential providers via User-Agent Feature ids
* **Dependency Update**: Updated to the latest SDK module versions

# v1.53.16 (2025-02-19)

* **Documentation**: This is a documentation only release for Amazon ECS that supports the CPU task limit increase.

# v1.53.15 (2025-02-18)

* **Bug Fix**: Bump go version to 1.22
* **Dependency Update**: Updated to the latest SDK module versions

# v1.53.14 (2025-02-13)

* **Documentation**: This is a documentation only release to support migrating Amazon ECS service ARNs to the long ARN format.

# v1.53.13 (2025-02-05)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.53.12 (2025-02-04)

* No change notes available for this release.

# v1.53.11 (2025-01-31)

* **Dependency Update**: Switch to code-generated waiter matchers, removing the dependency on go-jmespath.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.53.10 (2025-01-30)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.53.9 (2025-01-24)

* **Dependency Update**: Updated to the latest SDK module versions
* **Dependency Update**: Upgrade to smithy-go v1.22.2.

# v1.53.8 (2025-01-17)

* **Bug Fix**: Fix bug where credentials weren't refreshed during retry loop.

# v1.53.7 (2025-01-16)

* **Documentation**: The release addresses Amazon ECS documentation tickets.

# v1.53.6 (2025-01-15)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.53.5 (2025-01-14)

* **Bug Fix**: Fix issue where waiters were not failing on unmatched errors as they should. This may have breaking behavioral changes for users in fringe cases. See [this announcement](https://github.com/aws/aws-sdk-go-v2/discussions/2954) for more information.

# v1.53.4 (2025-01-09)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.53.3 (2025-01-08)

* No change notes available for this release.

# v1.53.2 (2025-01-03)

* **Documentation**: Adding SDK reference examples for Amazon ECS operations.

# v1.53.1 (2024-12-19)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.53.0 (2024-12-17)

* **Feature**: Added support for enableFaultInjection task definition parameter which can be used to enable Fault Injection feature on ECS tasks.

# v1.52.2 (2024-12-09)

* **Documentation**: This is a documentation only update to address various tickets for Amazon ECS.

# v1.52.1 (2024-12-02)

* **Documentation**: This release adds support for Container Insights with Enhanced Observability for Amazon ECS.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.52.0 (2024-11-20)

* **Feature**: This release adds support for the Availability Zone rebalancing feature on Amazon ECS.

# v1.51.0 (2024-11-19)

* **Feature**: This release introduces support for configuring the version consistency feature for individual containers defined within a task definition. The configuration allows to specify whether ECS should resolve the container image tag specified in the container definition to an image digest.

# v1.50.0 (2024-11-18)

* **Feature**: This release adds support for adding VPC Lattice configurations in ECS CreateService/UpdateService APIs. The configuration allows for associating VPC Lattice target groups with ECS Services.
* **Dependency Update**: Update to smithy-go v1.22.1.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.49.2 (2024-11-07)

* **Bug Fix**: Adds case-insensitive handling of error message fields in service responses

# v1.49.1 (2024-11-06)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.49.0 (2024-10-30)

* **Feature**: This release supports service deployments and service revisions which provide a comprehensive view of your Amazon ECS service history.

# v1.48.1 (2024-10-28)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.48.0 (2024-10-24)

* **Feature**: This release adds support for EBS volumes attached to Amazon ECS Windows tasks running on EC2 instances.

# v1.47.4 (2024-10-17)

* **Documentation**: This is an Amazon ECS documentation only update to address tickets.

# v1.47.3 (2024-10-10)

* **Documentation**: This is a documentation only release that updates to documentation to let customers know that Amazon Elastic Inference is no longer available.

# v1.47.2 (2024-10-08)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.47.1 (2024-10-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.47.0 (2024-10-04)

* **Feature**: Add support for HTTP client metrics.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.46.4 (2024-10-03)

* No change notes available for this release.

# v1.46.3 (2024-09-27)

* No change notes available for this release.

# v1.46.2 (2024-09-25)

* No change notes available for this release.

# v1.46.1 (2024-09-23)

* No change notes available for this release.

# v1.46.0 (2024-09-20)

* **Feature**: Add tracing and metrics support to service clients.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.45.5 (2024-09-17)

* **Bug Fix**: **BREAKFIX**: Only generate AccountIDEndpointMode config for services that use it. This is a compiler break, but removes no actual functionality, as no services currently use the account ID in endpoint resolution.
* **Documentation**: This is a documentation only release to address various tickets.

# v1.45.4 (2024-09-04)

* No change notes available for this release.

# v1.45.3 (2024-09-03)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.45.2 (2024-08-22)

* No change notes available for this release.

# v1.45.1 (2024-08-20)

* **Documentation**: Documentation only release to address various tickets

# v1.45.0 (2024-08-15)

* **Feature**: This release introduces a new ContainerDefinition configuration to support the customer-managed keys for ECS container restart feature.
* **Dependency Update**: Bump minimum Go version to 1.21.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.44.3 (2024-07-10.2)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.44.2 (2024-07-10)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.44.1 (2024-06-28)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.44.0 (2024-06-26)

* **Feature**: Support list-of-string endpoint parameter.

# v1.43.1 (2024-06-19)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.43.0 (2024-06-18)

* **Feature**: Track usage of various AWS SDK features in user-agent string.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.42.1 (2024-06-17)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.42.0 (2024-06-10)

* **Feature**: This release introduces a new cluster configuration to support the customer-managed keys for ECS managed storage encryption.

# v1.41.13 (2024-06-07)

* **Bug Fix**: Add clock skew correction on all service clients
* **Dependency Update**: Updated to the latest SDK module versions

# v1.41.12 (2024-06-03)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.41.11 (2024-05-23)

* No change notes available for this release.

# v1.41.10 (2024-05-16)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.41.9 (2024-05-15)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.41.8 (2024-05-08)

* **Bug Fix**: GoDoc improvement

# v1.41.7 (2024-04-02)

* **Documentation**: Documentation only update for Amazon ECS.

# v1.41.6 (2024-03-29)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.41.5 (2024-03-26)

* **Documentation**: This is a documentation update for Amazon ECS.

# v1.41.4 (2024-03-25)

* **Documentation**: Documentation only update for Amazon ECS.

# v1.41.3 (2024-03-18)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.41.2 (2024-03-07)

* **Bug Fix**: Remove dependency on go-cmp.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.41.1 (2024-02-23)

* **Bug Fix**: Move all common, SDK-side middleware stack ops into the service client module to prevent cross-module compatibility issues in the future.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.41.0 (2024-02-22)

* **Feature**: Add middleware stack snapshot tests.

# v1.40.2 (2024-02-21)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.40.1 (2024-02-20)

* **Bug Fix**: When sourcing values for a service's `EndpointParameters`, the lack of a configured region (i.e. `options.Region == ""`) will now translate to a `nil` value for `EndpointParameters.Region` instead of a pointer to the empty string `""`. This will result in a much more explicit error when calling an operation instead of an obscure hostname lookup failure.

# v1.40.0 (2024-02-16)

* **Feature**: Add new ClientOptions field to waiter config which allows you to extend the config for operation calls made by waiters.

# v1.39.1 (2024-02-15)

* **Bug Fix**: Correct failure to determine the error type in awsJson services that could occur when errors were modeled with a non-string `code` field.

# v1.39.0 (2024-02-13)

* **Feature**: Bump minimum Go version to 1.20 per our language support policy.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.38.3 (2024-02-09)

* **Documentation**: Documentation only update for Amazon ECS.

# v1.38.2 (2024-02-06)

* **Documentation**: This release is a documentation only update to address customer issues.

# v1.38.1 (2024-01-24)

* **Documentation**: Documentation updates for Amazon ECS.

# v1.38.0 (2024-01-22)

* **Feature**: This release adds support for Transport Layer Security (TLS) and Configurable Timeout to ECS Service Connect. TLS facilitates privacy and data security for inter-service communications, while Configurable Timeout allows customized per-request timeout and idle timeout for Service Connect services.

# v1.37.0 (2024-01-11)

* **Feature**: This release adds support for adding an ElasticBlockStorage volume configurations in ECS RunTask/StartTask/CreateService/UpdateService APIs. The configuration allows for attaching EBS volumes to ECS Tasks.

# v1.36.0 (2024-01-04)

* **Feature**: This release adds support for managed instance draining which facilitates graceful termination of Amazon ECS instances.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.35.6 (2023-12-20)

* No change notes available for this release.

# v1.35.5 (2023-12-08)

* **Bug Fix**: Reinstate presence of default Retryer in functional options, but still respect max attempts set therein.

# v1.35.4 (2023-12-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.35.3 (2023-12-06)

* **Bug Fix**: Restore pre-refactor auth behavior where all operations could technically be performed anonymously.

# v1.35.2 (2023-12-01)

* **Bug Fix**: Correct wrapping of errors in authentication workflow.
* **Bug Fix**: Correctly recognize cache-wrapped instances of AnonymousCredentials at client construction.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.35.1 (2023-11-30)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.35.0 (2023-11-29)

* **Feature**: Expose Options() accessor on service clients.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.34.2 (2023-11-28.2)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.34.1 (2023-11-28)

* **Bug Fix**: Respect setting RetryMaxAttempts in functional options at client construction.

# v1.34.0 (2023-11-27)

* **Feature**: Adds a new 'type' property to the Setting structure. Adds a new AccountSetting - guardDutyActivate for ECS.

# v1.33.2 (2023-11-20)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.33.1 (2023-11-15)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.33.0 (2023-11-13)

* **Feature**: Adds a Client Token parameter to the ECS RunTask API. The Client Token parameter allows for idempotent RunTask requests.

# v1.32.1 (2023-11-09)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.32.0 (2023-11-01)

* **Feature**: Adds support for configured endpoints via environment variables and the AWS shared configuration file.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.31.0 (2023-10-31)

* **Feature**: **BREAKING CHANGE**: Bump minimum go version to 1.19 per the revised [go version support policy](https://aws.amazon.com/blogs/developer/aws-sdk-for-go-aligns-with-go-release-policy-on-supported-runtimes/).
* **Dependency Update**: Updated to the latest SDK module versions

# v1.30.4 (2023-10-17)

* **Documentation**: Documentation only updates to address Amazon ECS tickets.

# v1.30.3 (2023-10-12)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.30.2 (2023-10-06)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.30.1 (2023-09-05)

* **Documentation**: Documentation only update for Amazon ECS.

# v1.30.0 (2023-08-31)

* **Feature**: This release adds support for an account-level setting that you can use to configure the number of days for AWS Fargate task retirement.

# v1.29.6 (2023-08-21)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.29.5 (2023-08-18)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.29.4 (2023-08-17)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.29.3 (2023-08-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.29.2 (2023-08-04)

* **Documentation**: This is a documentation update to address various tickets.

# v1.29.1 (2023-08-01)

* No change notes available for this release.

# v1.29.0 (2023-07-31)

* **Feature**: Adds support for smithy-modeled endpoint resolution. A new rules-based endpoint resolution will be added to the SDK which will supercede and deprecate existing endpoint resolution. Specifically, EndpointResolver will be deprecated while BaseEndpoint and EndpointResolverV2 will take its place. For more information, please see the Endpoints section in our Developer Guide.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.28.2 (2023-07-28)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.28.1 (2023-07-13)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.28.0 (2023-06-30)

* **Feature**: Added new field  "credentialspecs" to the ecs task definition to support gMSA of windows/linux in both domainless and domain-joined mode

# v1.27.4 (2023-06-19)

* **Documentation**: Documentation only update to address various tickets.

# v1.27.3 (2023-06-15)

* No change notes available for this release.

# v1.27.2 (2023-06-13)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.27.1 (2023-05-18)

* **Documentation**: Documentation only release to address various tickets.

# v1.27.0 (2023-05-04)

* **Feature**: Documentation update for new error type NamespaceNotFoundException for CreateCluster and UpdateCluster

# v1.26.3 (2023-05-02)

* **Documentation**: Documentation only update to address Amazon ECS tickets.

# v1.26.2 (2023-04-24)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.26.1 (2023-04-21)

* **Documentation**: Documentation update to address various Amazon ECS tickets.

# v1.26.0 (2023-04-19)

* **Feature**: This release supports the Account Setting "TagResourceAuthorization" that allows for enhanced Tagging security controls.

# v1.25.1 (2023-04-14)

* **Documentation**: This release supports  ephemeral storage for AWS Fargate Windows containers.

# v1.25.0 (2023-04-10)

* **Feature**: This release adds support for enabling FIPS compliance on Amazon ECS Fargate tasks

# v1.24.4 (2023-04-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.24.3 (2023-04-05)

* **Documentation**: This is a document only updated to add information about Amazon Elastic Inference (EI).

# v1.24.2 (2023-03-21)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.24.1 (2023-03-10)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.24.0 (2023-02-23)

* **Feature**: This release supports deleting Amazon ECS task definitions that are in the INACTIVE state.

# v1.23.5 (2023-02-22)

* **Bug Fix**: Prevent nil pointer dereference when retrieving error codes.

# v1.23.4 (2023-02-20)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.23.3 (2023-02-15)

* **Announcement**: When receiving an error response in restJson-based services, an incorrect error type may have been returned based on the content of the response. This has been fixed via PR #2012 tracked in issue #1910.
* **Bug Fix**: Correct error type parsing for restJson services.

# v1.23.2 (2023-02-03)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.23.1 (2023-01-23)

* No change notes available for this release.

# v1.23.0 (2023-01-05)

* **Feature**: Add `ErrorCodeOverride` field to all error structs (aws/smithy-go#401).

# v1.22.0 (2022-12-19)

* **Feature**: This release adds support for alarm-based rollbacks in ECS, a new feature that allows customers to add automated safeguards for Amazon ECS service rolling updates.

# v1.21.0 (2022-12-15)

* **Feature**: This release adds support for container port ranges in ECS, a new capability that allows customers to provide container port ranges to simplify use cases where multiple ports are in use in a container. This release updates TaskDefinition mutation APIs and the Task description APIs.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.20.1 (2022-12-02)

* **Documentation**: Documentation updates for Amazon ECS
* **Dependency Update**: Updated to the latest SDK module versions

# v1.20.0 (2022-11-28)

* **Feature**: This release adds support for ECS Service Connect, a new capability that simplifies writing and operating resilient distributed applications. This release updates the TaskDefinition, Cluster, Service mutation APIs with Service connect constructs and also adds a new ListServicesByNamespace API.

# v1.19.2 (2022-11-22)

* No change notes available for this release.

# v1.19.1 (2022-11-16)

* No change notes available for this release.

# v1.19.0 (2022-11-10)

* **Feature**: This release adds support for task scale-in protection with updateTaskProtection and getTaskProtection APIs. UpdateTaskProtection API can be used to protect a service managed task from being terminated by scale-in events and getTaskProtection API to get the scale-in protection status of a task.

# v1.18.26 (2022-10-24)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.25 (2022-10-21)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.24 (2022-10-13)

* **Documentation**: Documentation update to address tickets.

# v1.18.23 (2022-10-04)

* **Documentation**: Documentation updates to address various Amazon ECS tickets.

# v1.18.22 (2022-09-20)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.21 (2022-09-16)

* **Documentation**: This release supports new task definition sizes.

# v1.18.20 (2022-09-14)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.19 (2022-09-02)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.18 (2022-08-31)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.17 (2022-08-30)

* No change notes available for this release.

# v1.18.16 (2022-08-29)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.15 (2022-08-11)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.14 (2022-08-09)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.13 (2022-08-08)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.12 (2022-08-01)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.11 (2022-07-05)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.10 (2022-06-29)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.9 (2022-06-21)

* **Documentation**: Amazon ECS UpdateService now supports the following parameters: PlacementStrategies, PlacementConstraints and CapacityProviderStrategy.

# v1.18.8 (2022-06-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.7 (2022-05-17)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.6 (2022-04-25)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.5 (2022-03-30)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.4 (2022-03-24)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.3 (2022-03-23)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.2 (2022-03-22)

* **Documentation**: Documentation only update to address tickets

# v1.18.1 (2022-03-15)

* **Documentation**: Documentation only update to address tickets

# v1.18.0 (2022-03-08)

* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Feature**: Updated service client model to latest release.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.17.0 (2022-02-24)

* **Feature**: API client updated
* **Feature**: Adds RetryMaxAttempts and RetryMod to API client Options. This allows the API clients' default Retryer to be configured from the shared configuration files or environment variables. Adding a new Retry mode of `Adaptive`. `Adaptive` retry mode is an experimental mode, adding client rate limiting when throttles reponses are received from an API. See [retry.AdaptiveMode](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/aws/retry#AdaptiveMode) for more details, and configuration options.
* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.16.0 (2022-01-14)

* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.15.0 (2022-01-07)

* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Documentation**: API client updated
* **Dependency Update**: Updated to the latest SDK module versions

# v1.14.0 (2021-12-21)

* **Feature**: API Paginators now support specifying the initial starting token, and support stopping on empty string tokens.
* **Feature**: Updated to latest service endpoints

# v1.13.1 (2021-12-02)

* **Bug Fix**: Fixes a bug that prevented aws.EndpointResolverWithOptions from being used by the service client. ([#1514](https://github.com/aws/aws-sdk-go-v2/pull/1514))
* **Dependency Update**: Updated to the latest SDK module versions

# v1.13.0 (2021-11-30)

* **Feature**: API client updated

# v1.12.1 (2021-11-19)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.12.0 (2021-11-12)

* **Feature**: Service clients now support custom endpoints that have an initial URI path defined.
* **Feature**: Updated service to latest API model.
* **Feature**: Waiters now have a `WaitForOutput` method, which can be used to retrieve the output of the successful wait operation. Thank you to [Andrew Haines](https://github.com/haines) for contributing this feature.

# v1.11.0 (2021-11-06)

* **Feature**: The SDK now supports configuration of FIPS and DualStack endpoints using environment variables, shared configuration, or programmatically.
* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Feature**: Updated service to latest API model.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.10.0 (2021-10-21)

* **Feature**: API client updated
* **Feature**: Updated  to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.9.2 (2021-10-11)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.9.1 (2021-09-17)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.9.0 (2021-08-27)

* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.8.1 (2021-08-19)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.8.0 (2021-08-12)

* **Feature**: API client updated

# v1.7.0 (2021-08-04)

* **Feature**: Updated to latest API model.
* **Dependency Update**: Updated `github.com/aws/smithy-go` to latest version.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.6.1 (2021-07-15)

* **Dependency Update**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.6.0 (2021-06-25)

* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.5.0 (2021-06-04)

* **Feature**: Updated service client to latest API model.

# v1.4.1 (2021-05-20)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.4.0 (2021-05-14)

* **Feature**: Constant has been added to modules to enable runtime version inspection for reporting.
* **Feature**: Updated to latest service API model.
* **Dependency Update**: Updated to the latest SDK module versions

