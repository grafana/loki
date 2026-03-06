# v1.116.1 (2026-02-23)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.116.0 (2026-02-17)

* **Feature**: Adds support for the StorageEncryptionType field to specify encryption type for DB clusters, DB instances, snapshots, automated backups, and global clusters.

# v1.115.0 (2026-02-10.2)

* **Feature**: This release adds backup configuration for RDS and Aurora restores, letting customers set backup retention period and preferred backup window during restore. It also enables viewing backup settings when describing snapshots or automated backups for instances and clusters.

# v1.114.0 (2026-01-14)

* **Feature**: no feature changes. model migrated to Smithy

# v1.113.2 (2026-01-09)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.113.1 (2025-12-09)

* No change notes available for this release.

# v1.113.0 (2025-12-08)

* **Feature**: Adding support for tagging RDS Instance/Cluster Automated Backups
* **Dependency Update**: Updated to the latest SDK module versions

# v1.112.0 (2025-12-02)

* **Feature**: RDS Oracle and SQL Server: Add support for adding, modifying, and removing additional storage volumes, offering up to 256TiB storage; RDS SQL Server: Support Developer Edition via custom engine versions for development and testing purposes; M7i/R7i instances with Optimize CPU for cost savings.
* **Dependency Update**: Updated to the latest SDK module versions
* **Dependency Update**: Upgrade to smithy-go v1.24.0. Notably this version of the library reduces the allocation footprint of the middleware system. We observe a ~10% reduction in allocations per SDK call with this change.

# v1.111.1 (2025-11-25)

* **Bug Fix**: Add error check for endpoint param binding during auth scheme resolution to fix panic reported in #3234

# v1.111.0 (2025-11-21)

* **Feature**: Add support for Upgrade Rollout Order

# v1.110.0 (2025-11-20)

* **Feature**: Add support for VPC Encryption Controls.

# v1.109.1 (2025-11-19.2)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.109.0 (2025-11-13)

* **Feature**: Updated endpoint and service metadata

# v1.108.9 (2025-11-12)

* **Bug Fix**: Further reduce allocation overhead when the metrics system isn't in-use.
* **Bug Fix**: Reduce allocation overhead when the client doesn't have any HTTP interceptors configured.
* **Bug Fix**: Remove blank trace spans towards the beginning of the request that added no additional information. This conveys a slight reduction in overall allocations.

# v1.108.8 (2025-11-11)

* **Bug Fix**: Return validation error if input region is not a valid host label.

# v1.108.7 (2025-11-04)

* **Dependency Update**: Updated to the latest SDK module versions
* **Dependency Update**: Upgrade to smithy-go v1.23.2 which should convey some passive reduction of overall allocations, especially when not using the metrics system.

# v1.108.6 (2025-10-30)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.108.5 (2025-10-23)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.108.4 (2025-10-22)

* No change notes available for this release.

# v1.108.3 (2025-10-16)

* **Dependency Update**: Bump minimum Go version to 1.23.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.108.2 (2025-10-10)

* **Documentation**: Updated the text in the Important section of the ModifyDBClusterParameterGroup page.

# v1.108.1 (2025-10-06)

* **Documentation**: Documentation updates to the CreateDBClusterMessage$PubliclyAccessible and CreateDBInstanceMessage$PubliclyAccessible properties.

# v1.108.0 (2025-09-30)

* **Feature**: Enhanced RDS error handling: Added DBProxyEndpointNotFoundFault, DBShardGroupNotFoundFault, KMSKeyNotAccessibleFault for snapshots/restores/backups, NetworkTypeNotSupported, StorageTypeNotSupportedFault for restores, and granular state validation faults. Changed DBInstanceNotReadyFault to HTTP 400.

# v1.107.2 (2025-09-26)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.107.1 (2025-09-23)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.107.0 (2025-09-11)

* **Feature**: Adds support for end-to-end IAM authentication in RDS Proxy for MySQL, MariaDB, and PostgreSQL engines.

# v1.106.2 (2025-09-10)

* No change notes available for this release.

# v1.106.1 (2025-09-08)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.106.0 (2025-09-04)

* **Feature**: Added new EndpointNetworkType and TargetConnectionNetworkType fields in Proxy APIs to support IPv6

# v1.105.0 (2025-09-03)

* **Feature**: This release adds support for MasterUserAuthenticationType parameter on CreateDBInstance, ModifyDBInstance, CreateDBCluster, and ModifyDBCluster operations.

# v1.104.1 (2025-08-29)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.104.0 (2025-08-28)

* **Feature**: Added RDS HTTP Endpoint feature support flag to DescribeOrderableDBInstanceOptions API

# v1.103.4 (2025-08-27)

* **Dependency Update**: Update to smithy-go v1.23.0.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.103.3 (2025-08-22)

* **Documentation**: Updates Amazon RDS documentation for Db2 read-only replicas.

# v1.103.2 (2025-08-21)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.103.1 (2025-08-20)

* **Bug Fix**: Remove unused deserialization code.

# v1.103.0 (2025-08-11)

* **Feature**: Add support for configuring per-service Options via callback on global config.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.102.0 (2025-08-05)

* **Feature**: Adds a new Aurora Serverless v2 attribute to the DBCluster resource to expose the platform version. Also updates the attribute to be part of both the engine version and platform version descriptions.

# v1.101.0 (2025-08-04)

* **Feature**: Support configurable auth scheme preferences in service clients via AWS_AUTH_SCHEME_PREFERENCE in the environment, auth_scheme_preference in the config file, and through in-code settings on LoadDefaultConfig and client constructor methods.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.100.1 (2025-07-30)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.100.0 (2025-07-28)

* **Feature**: Add support for HTTP interceptors.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.99.2 (2025-07-19)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.99.1 (2025-07-01)

* **Documentation**: Amazon RDS Custom for Oracle now supports multi-AZ database instances.

# v1.99.0 (2025-06-27)

* **Feature**: StartDBCluster and StopDBCluster can now throw InvalidDBShardGroupStateFault.

# v1.98.0 (2025-06-24)

* **Feature**: Adding support for RDS on Dedicated Local Zones, including local backup target, snapshot availability zone and snapshot target

# v1.97.3 (2025-06-17)

* **Dependency Update**: Update to smithy-go v1.22.4.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.97.2 (2025-06-11)

* **Documentation**: Updates Amazon RDS documentation for Amazon RDS for Db2 cross-Region replicas in standby mode.

# v1.97.1 (2025-06-10)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.97.0 (2025-06-06)

* **Feature**: Include Global Cluster Identifier in DBCluster if the DBCluster is a Global Cluster Member.

# v1.96.0 (2025-05-20)

* **Feature**: This release introduces the new DescribeDBMajorEngineVersions API for describing the properties of specific major versions of database engines.

# v1.95.0 (2025-04-24)

* **Feature**: This Amazon RDS release adds support for managed master user passwords for Oracle CDBs.

# v1.94.4 (2025-04-10)

* No change notes available for this release.

# v1.94.3 (2025-04-03)

* No change notes available for this release.

# v1.94.2 (2025-03-26)

* **Documentation**: Add note about the Availability Zone where RDS restores the DB cluster for the RestoreDBClusterToPointInTime operation.

# v1.94.1 (2025-03-04.2)

* **Bug Fix**: Add assurance test for operation order.
* **Documentation**: Note support for Database Insights for Amazon RDS.

# v1.94.0 (2025-02-27)

* **Feature**: Track credential providers via User-Agent Feature ids
* **Dependency Update**: Updated to the latest SDK module versions

# v1.93.14 (2025-02-20)

* **Documentation**: CloudWatch Database Insights now supports Amazon RDS.

# v1.93.13 (2025-02-18)

* **Bug Fix**: Bump go version to 1.22
* **Dependency Update**: Updated to the latest SDK module versions

# v1.93.12 (2025-02-05)

* **Documentation**: Documentation updates to clarify the description for the parameter AllocatedStorage for the DB cluster data type, the description for the parameter DeleteAutomatedBackups for the DeleteDBCluster API operation, and removing an outdated note for the CreateDBParameterGroup API operation.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.93.11 (2025-02-04)

* No change notes available for this release.

# v1.93.10 (2025-01-31)

* **Documentation**: Updates to Aurora MySQL and Aurora PostgreSQL API pages with instance log type in the create and modify DB Cluster.
* **Dependency Update**: Switch to code-generated waiter matchers, removing the dependency on go-jmespath.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.93.9 (2025-01-30)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.93.8 (2025-01-24)

* **Dependency Update**: Updated to the latest SDK module versions
* **Dependency Update**: Upgrade to smithy-go v1.22.2.

# v1.93.7 (2025-01-17)

* **Bug Fix**: Fix bug where credentials weren't refreshed during retry loop.

# v1.93.6 (2025-01-15)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.93.5 (2025-01-14)

* **Bug Fix**: Fix issue where waiters were not failing on unmatched errors as they should. This may have breaking behavioral changes for users in fringe cases. See [this announcement](https://github.com/aws/aws-sdk-go-v2/discussions/2954) for more information.

# v1.93.4 (2025-01-09)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.93.3 (2025-01-08)

* **Documentation**: Updates Amazon RDS documentation to clarify the RestoreDBClusterToPointInTime description.

# v1.93.2 (2024-12-27)

* **Documentation**: Updates Amazon RDS documentation to correct various descriptions.

# v1.93.1 (2024-12-19)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.93.0 (2024-12-16)

* **Feature**: This release adds support for the "MYSQL_CACHING_SHA2_PASSWORD" enum value for RDS Proxy ClientPasswordAuthType.

# v1.92.0 (2024-12-02)

* **Feature**: Amazon RDS supports CloudWatch Database Insights. You can use the SDK to create, modify, and describe the DatabaseInsightsMode for your DB instances and clusters.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.91.0 (2024-11-20)

* **Feature**: This release adds support for scale storage on the DB instance using a Blue/Green Deployment.

# v1.90.0 (2024-11-18)

* **Feature**: Add support for the automatic pause/resume feature of Aurora Serverless v2.
* **Dependency Update**: Update to smithy-go v1.22.1.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.89.2 (2024-11-12)

* **Documentation**: Updates Amazon RDS documentation for Amazon RDS Extended Support for Amazon Aurora MySQL.

# v1.89.1 (2024-11-06)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.89.0 (2024-10-28)

* **Feature**: This release adds support for Enhanced Monitoring and Performance Insights when restoring Aurora Limitless Database DB clusters. It also adds support for the os-upgrade pending maintenance action.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.88.0 (2024-10-22)

* **Feature**: Global clusters now expose the Endpoint attribute as one of its fields. It is a Read/Write endpoint for the global cluster which resolves to the Global Cluster writer instance.

# v1.87.3 (2024-10-17)

* **Documentation**: Updates Amazon RDS documentation for TAZ IAM support

# v1.87.2 (2024-10-08)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.87.1 (2024-10-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.87.0 (2024-10-04)

* **Feature**: Add support for HTTP client metrics.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.86.1 (2024-10-03)

* No change notes available for this release.

# v1.86.0 (2024-10-01)

* **Feature**: This release provides additional support for enabling Aurora Limitless Database DB clusters.

# v1.85.2 (2024-09-27)

* No change notes available for this release.

# v1.85.1 (2024-09-25)

* No change notes available for this release.

# v1.85.0 (2024-09-23)

* **Feature**: Support ComputeRedundancy parameter in ModifyDBShardGroup API. Add DBShardGroupArn in DBShardGroup API response. Remove InvalidMaxAcuFault from CreateDBShardGroup and ModifyDBShardGroup API. Both API will throw InvalidParameterValueException for invalid ACU configuration.

# v1.84.0 (2024-09-20)

* **Feature**: Add tracing and metrics support to service clients.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.83.2 (2024-09-18)

* **Documentation**: Updates Amazon RDS documentation with information upgrading snapshots with unsupported engine versions for RDS for MySQL and RDS for PostgreSQL.

# v1.83.1 (2024-09-17)

* **Bug Fix**: **BREAKFIX**: Only generate AccountIDEndpointMode config for services that use it. This is a compiler break, but removes no actual functionality, as no services currently use the account ID in endpoint resolution.
* **Documentation**: Updates Amazon RDS documentation with configuration information about the BYOL model for RDS for Db2.

# v1.83.0 (2024-09-16)

* **Feature**: Launching Global Cluster tagging.

# v1.82.5 (2024-09-12)

* **Documentation**: This release adds support for the os-upgrade pending maintenance action for Amazon Aurora DB clusters.

# v1.82.4 (2024-09-04)

* No change notes available for this release.

# v1.82.3 (2024-09-03)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.82.2 (2024-08-22)

* No change notes available for this release.

# v1.82.1 (2024-08-15)

* **Dependency Update**: Bump minimum Go version to 1.21.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.82.0 (2024-08-01)

* **Feature**: This release adds support for specifying optional MinACU parameter in CreateDBShardGroup and ModifyDBShardGroup API. DBShardGroup response will contain MinACU if specified.

# v1.81.5 (2024-07-18)

* **Documentation**: Updates Amazon RDS documentation to specify an eventual consistency model for DescribePendingMaintenanceActions.

# v1.81.4 (2024-07-10.2)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.81.3 (2024-07-10)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.81.2 (2024-06-28)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.81.1 (2024-06-27)

* **Documentation**: Updates Amazon RDS documentation for TAZ export to S3.

# v1.81.0 (2024-06-26)

* **Feature**: Support list-of-string endpoint parameter.

# v1.80.1 (2024-06-19)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.80.0 (2024-06-18)

* **Feature**: Track usage of various AWS SDK features in user-agent string.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.79.7 (2024-06-17)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.79.6 (2024-06-07)

* **Bug Fix**: Add clock skew correction on all service clients
* **Dependency Update**: Updated to the latest SDK module versions

# v1.79.5 (2024-06-04)

* No change notes available for this release.

# v1.79.4 (2024-06-03)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.79.3 (2024-05-30)

* **Documentation**: Updates Amazon RDS documentation for Aurora Postgres DBname.

# v1.79.2 (2024-05-23)

* No change notes available for this release.

# v1.79.1 (2024-05-21)

* **Documentation**: Updates Amazon RDS documentation for Db2 license through AWS Marketplace.

# v1.79.0 (2024-05-20)

* **Feature**: This release adds support for EngineLifecycleSupport on DBInstances, DBClusters, and GlobalClusters.

# v1.78.3 (2024-05-16)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.78.2 (2024-05-15)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.78.1 (2024-05-08)

* **Bug Fix**: GoDoc improvement

# v1.78.0 (2024-04-26)

* **Feature**: SupportsLimitlessDatabase field added to describe-db-engine-versions to indicate whether the DB engine version supports Aurora Limitless Database.

# v1.77.3 (2024-04-25)

* **Documentation**: Updates Amazon RDS documentation for setting local time zones for RDS for Db2 DB instances.

# v1.77.2 (2024-04-23)

* **Documentation**: Fix the example ARN for ModifyActivityStreamRequest

# v1.77.1 (2024-04-11)

* **Documentation**: Updates Amazon RDS documentation for Standard Edition 2 support in RDS Custom for Oracle.

# v1.77.0 (2024-04-09)

* **Feature**: This release adds support for specifying the CA certificate to use for the new db instance when restoring from db snapshot, restoring from s3, restoring to point in time, and creating a db instance read replica.

# v1.76.1 (2024-03-29)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.76.0 (2024-03-18)

* **Feature**: This release launches the ModifyIntegration API and support for data filtering for zero-ETL Integrations.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.75.2 (2024-03-14)

* **Documentation**: Updates Amazon RDS documentation for EBCDIC collation for RDS for Db2.

# v1.75.1 (2024-03-07)

* **Bug Fix**: Remove dependency on go-cmp.
* **Documentation**: Updates Amazon RDS documentation for io2 storage for Multi-AZ DB clusters
* **Dependency Update**: Updated to the latest SDK module versions

# v1.75.0 (2024-03-06)

* **Feature**: Updated the input of CreateDBCluster and ModifyDBCluster to support setting CA certificates. Updated the output of DescribeDBCluster to show current CA certificate setting value.

# v1.74.2 (2024-03-05)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.74.1 (2024-03-04)

* **Bug Fix**: Update internal/presigned-url dependency for corrected API name.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.74.0 (2024-02-26)

* **Feature**: This release adds support for gp3 data volumes for Multi-AZ DB Clusters.

# v1.73.0 (2024-02-23)

* **Feature**: Add pattern and length based validations for DBShardGroupIdentifier
* **Bug Fix**: Move all common, SDK-side middleware stack ops into the service client module to prevent cross-module compatibility issues in the future.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.72.0 (2024-02-22)

* **Feature**: Add middleware stack snapshot tests.

# v1.71.2 (2024-02-21)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.71.1 (2024-02-20)

* **Bug Fix**: When sourcing values for a service's `EndpointParameters`, the lack of a configured region (i.e. `options.Region == ""`) will now translate to a `nil` value for `EndpointParameters.Region` instead of a pointer to the empty string `""`. This will result in a much more explicit error when calling an operation instead of an obscure hostname lookup failure.

# v1.71.0 (2024-02-16)

* **Feature**: Add new ClientOptions field to waiter config which allows you to extend the config for operation calls made by waiters.
* **Documentation**: Doc only update for a valid option in DB parameter group

# v1.70.0 (2024-02-13)

* **Feature**: Bump minimum Go version to 1.20 per our language support policy.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.69.0 (2024-01-29)

* **Feature**: Introduced support for the InsufficientDBInstanceCapacityFault error in the RDS RestoreDBClusterFromSnapshot and RestoreDBClusterToPointInTime API methods. This provides enhanced error handling, ensuring a more robust experience.

# v1.68.0 (2024-01-24)

* **Feature**: This release adds support for Aurora Limitless Database.

# v1.67.0 (2024-01-22)

* **Feature**: Introduced support for the InsufficientDBInstanceCapacityFault error in the RDS CreateDBCluster API method. This provides enhanced error handling, ensuring a more robust experience when creating database clusters with insufficient instance capacity.

# v1.66.2 (2024-01-04)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.66.1 (2023-12-27)

* No change notes available for this release.

# v1.66.0 (2023-12-21)

* **Feature**: This release adds support for using RDS Data API with Aurora PostgreSQL Serverless v2 and provisioned DB clusters.

# v1.65.0 (2023-12-19)

* **Feature**: RDS - The release adds two new APIs: DescribeDBRecommendations and ModifyDBRecommendation

# v1.64.6 (2023-12-15)

* **Documentation**: Updates Amazon RDS documentation by adding code examples

# v1.64.5 (2023-12-08)

* **Bug Fix**: Reinstate presence of default Retryer in functional options, but still respect max attempts set therein.

# v1.64.4 (2023-12-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.64.3 (2023-12-06)

* **Bug Fix**: Restore pre-refactor auth behavior where all operations could technically be performed anonymously.

# v1.64.2 (2023-12-01)

* **Bug Fix**: Correct wrapping of errors in authentication workflow.
* **Bug Fix**: Correctly recognize cache-wrapped instances of AnonymousCredentials at client construction.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.64.1 (2023-11-30)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.64.0 (2023-11-29)

* **Feature**: Expose Options() accessor on service clients.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.63.5 (2023-11-28.2)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.63.4 (2023-11-28)

* **Bug Fix**: Respect setting RetryMaxAttempts in functional options at client construction.

# v1.63.3 (2023-11-27.2)

* **Documentation**: Updates Amazon RDS documentation for support for RDS for Db2.

# v1.63.2 (2023-11-27)

* No change notes available for this release.

# v1.63.1 (2023-11-20)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.63.0 (2023-11-17)

* **Feature**: This release adds support for option groups and replica enhancements to Amazon RDS Custom.

# v1.62.4 (2023-11-15)

* **Documentation**: Updates Amazon RDS documentation for support for upgrading RDS for MySQL snapshots from version 5.7 to version 8.0.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.62.3 (2023-11-10)

* **Documentation**: Updates Amazon RDS documentation for zero-ETL integrations.

# v1.62.2 (2023-11-09)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.62.1 (2023-11-08)

* **Documentation**: This Amazon RDS release adds support for patching the OS of an RDS Custom for Oracle DB instance. You can now upgrade the database or operating system using the modify-db-instance command.

# v1.62.0 (2023-11-07)

* **Feature**: This Amazon RDS release adds support for the multi-tenant configuration. In this configuration, an RDS DB instance can contain multiple tenant databases. In RDS for Oracle, a tenant database is a pluggable database (PDB).

# v1.61.0 (2023-11-01)

* **Feature**: Adds support for configured endpoints via environment variables and the AWS shared configuration file.
* **Feature**: This release adds support for customized networking resources to Amazon RDS Custom.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.60.0 (2023-10-31)

* **Feature**: **BREAKING CHANGE**: Bump minimum go version to 1.19 per the revised [go version support policy](https://aws.amazon.com/blogs/developer/aws-sdk-for-go-aligns-with-go-release-policy-on-supported-runtimes/).
* **Dependency Update**: Updated to the latest SDK module versions

# v1.59.0 (2023-10-30)

* **Feature**: This release launches the CreateIntegration, DeleteIntegration, and DescribeIntegrations APIs to manage zero-ETL Integrations.

# v1.58.0 (2023-10-24)

* **Feature**: **BREAKFIX**: Correct nullability and default value representation of various input fields across a large number of services. Calling code that references one or more of the affected fields will need to update usage accordingly. See [2162](https://github.com/aws/aws-sdk-go-v2/issues/2162).

# v1.57.0 (2023-10-18)

* **Feature**: This release adds support for upgrading the storage file system configuration on the DB instance using a blue/green deployment or a read replica.

# v1.56.0 (2023-10-12)

* **Feature**: This release adds support for adding a dedicated log volume to open-source RDS instances.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.55.2 (2023-10-06)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.55.1 (2023-10-05)

* **Documentation**: Updates Amazon RDS documentation for corrections and minor improvements.

# v1.55.0 (2023-10-02)

* **Feature**: Adds DefaultCertificateForNewLaunches field in the DescribeCertificates API response.

# v1.54.0 (2023-09-05)

* **Feature**: Add support for feature integration with AWS Backup.

# v1.53.0 (2023-08-24)

* **Feature**: This release updates the supported versions for Percona XtraBackup in Aurora MySQL.

# v1.52.0 (2023-08-22)

* **Feature**: Adding parameters to CreateCustomDbEngineVersion reserved for future use.

# v1.51.0 (2023-08-21)

* **Feature**: Adding support for RDS Aurora Global Database Unplanned Failover
* **Dependency Update**: Updated to the latest SDK module versions

# v1.50.3 (2023-08-18)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.50.2 (2023-08-17)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.50.1 (2023-08-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.50.0 (2023-08-01)

* **Feature**: Added support for deleted clusters PiTR.

# v1.49.0 (2023-07-31)

* **Feature**: Adds support for smithy-modeled endpoint resolution. A new rules-based endpoint resolution will be added to the SDK which will supercede and deprecate existing endpoint resolution. Specifically, EndpointResolver will be deprecated while BaseEndpoint and EndpointResolverV2 will take its place. For more information, please see the Endpoints section in our Developer Guide.
* **Feature**: This release adds support for Aurora MySQL local write forwarding, which allows for forwarding of write operations from reader DB instances to the writer DB instance.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.48.1 (2023-07-28)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.48.0 (2023-07-25)

* **Feature**: This release adds support for monitoring storage optimization progress on the DescribeDBInstances API.

# v1.47.0 (2023-07-21)

* **Feature**: Adds support for the DBSystemID parameter of CreateDBInstance to RDS Custom for Oracle.

# v1.46.2 (2023-07-13)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.46.1 (2023-07-06)

* **Documentation**: Updates Amazon RDS documentation for creating DB instances and creating Aurora global clusters.

# v1.46.0 (2023-06-28)

* **Feature**: Amazon Relational Database Service (RDS) now supports joining a RDS for SQL Server instance to a self-managed Active Directory.

# v1.45.3 (2023-06-23)

* **Documentation**: Documentation improvements for create, describe, and modify DB clusters and DB instances.

# v1.45.2 (2023-06-15)

* No change notes available for this release.

# v1.45.1 (2023-06-13)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.45.0 (2023-05-31)

* **Feature**: This release adds support for changing the engine for Oracle using the ModifyDbInstance API

# v1.44.1 (2023-05-18)

* **Documentation**: RDS documentation update for the EngineVersion parameter of ModifyDBSnapshot

# v1.44.0 (2023-05-10)

* **Feature**: Amazon Relational Database Service (RDS) updates for the new Aurora I/O-Optimized storage type for Amazon Aurora DB clusters

# v1.43.3 (2023-05-04)

* No change notes available for this release.

# v1.43.2 (2023-04-24)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.43.1 (2023-04-19)

* **Documentation**: Adds support for the ImageId parameter of CreateCustomDBEngineVersion to RDS Custom for Oracle

# v1.43.0 (2023-04-14)

* **Feature**: This release adds support of modifying the engine mode of database clusters.

# v1.42.3 (2023-04-10)

* No change notes available for this release.

# v1.42.2 (2023-04-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.42.1 (2023-04-06)

* **Documentation**: Adds and updates the SDK examples

# v1.42.0 (2023-03-29)

* **Feature**: Add support for creating a read replica DB instance from a Multi-AZ DB cluster.

# v1.41.0 (2023-03-24)

* **Feature**: Added error code CreateCustomDBEngineVersionFault for when the create custom engine version for Custom engines fails.

# v1.40.7 (2023-03-21)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.40.6 (2023-03-10)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.40.5 (2023-02-22)

* **Bug Fix**: Prevent nil pointer dereference when retrieving error codes.

# v1.40.4 (2023-02-20)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.40.3 (2023-02-15)

* **Documentation**: Database Activity Stream support for RDS for SQL Server.

# v1.40.2 (2023-02-03)

* **Dependency Update**: Updated to the latest SDK module versions
* **Dependency Update**: Upgrade smithy to 1.27.2 and correct empty query list serialization.

# v1.40.1 (2023-01-23)

* No change notes available for this release.

# v1.40.0 (2023-01-10)

* **Feature**: This release adds support for configuring allocated storage on the CreateDBInstanceReadReplica, RestoreDBInstanceFromDBSnapshot, and RestoreDBInstanceToPointInTime APIs.

# v1.39.0 (2023-01-05)

* **Feature**: Add `ErrorCodeOverride` field to all error structs (aws/smithy-go#401).
* **Feature**: This release adds support for specifying which certificate authority (CA) to use for a DB instance's server certificate during DB instance creation, as well as other CA enhancements.

# v1.38.0 (2022-12-28)

* **Feature**: This release adds support for Custom Engine Version (CEV) on RDS Custom SQL Server.

# v1.37.0 (2022-12-22)

* **Feature**: Add support for managing master user password in AWS Secrets Manager for the DBInstance and DBCluster.

# v1.36.0 (2022-12-19)

* **Feature**: Add support for --enable-customer-owned-ip to RDS create-db-instance-read-replica API for RDS on Outposts.

# v1.35.1 (2022-12-15)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.35.0 (2022-12-13)

* **Feature**: This deployment adds ClientPasswordAuthType field to the Auth structure of the DBProxy.

# v1.34.0 (2022-12-12)

* **Feature**: Update the RDS API model to support copying option groups during the CopyDBSnapshot operation

# v1.33.0 (2022-12-06)

* **Feature**: This release adds the BlueGreenDeploymentNotFoundFault to the AddTagsToResource, ListTagsForResource, and RemoveTagsFromResource operations.

# v1.32.0 (2022-12-05)

* **Feature**: This release adds the InvalidDBInstanceStateFault to the RestoreDBClusterFromSnapshot operation.

# v1.31.1 (2022-12-02)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.31.0 (2022-11-28)

* **Feature**: This release enables new Aurora and RDS feature called Blue/Green Deployments that makes updates to databases safer, simpler and faster.

# v1.30.1 (2022-11-22)

* No change notes available for this release.

# v1.30.0 (2022-11-16)

* **Feature**: This release adds support for container databases (CDBs) to Amazon RDS Custom for Oracle. A CDB contains one PDB at creation. You can add more PDBs using Oracle SQL. You can also customize your database installation by setting the Oracle base, Oracle home, and the OS user name and group.

# v1.29.0 (2022-11-14)

* **Feature**: This release adds support for restoring an RDS Multi-AZ DB cluster snapshot to a Single-AZ deployment or a Multi-AZ DB instance deployment.

# v1.28.1 (2022-11-10)

* No change notes available for this release.

# v1.28.0 (2022-11-01)

* **Feature**: Relational Database Service - This release adds support for configuring Storage Throughput on RDS database instances.

# v1.27.0 (2022-10-25)

* **Feature**: Relational Database Service - This release adds support for exporting DB cluster data to Amazon S3.

# v1.26.3 (2022-10-24)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.26.2 (2022-10-21)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.26.1 (2022-09-20)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.26.0 (2022-09-19)

* **Feature**: This release adds support for Amazon RDS Proxy with SQL Server compatibility.

# v1.25.6 (2022-09-14)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.25.5 (2022-09-02)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.25.4 (2022-08-31)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.25.3 (2022-08-30)

* No change notes available for this release.

# v1.25.2 (2022-08-29)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.25.1 (2022-08-26)

* **Documentation**: Removes support for RDS Custom from DBInstanceClass in ModifyDBInstance

# v1.25.0 (2022-08-23)

* **Feature**: RDS for Oracle supports Oracle Data Guard switchover and read replica backups.

# v1.24.0 (2022-08-17)

* **Feature**: Adds support for Internet Protocol Version 6 (IPv6) for RDS Aurora database clusters.

# v1.23.6 (2022-08-14)

* **Documentation**: Adds support for RDS Custom to DBInstanceClass in ModifyDBInstance

# v1.23.5 (2022-08-11)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.23.4 (2022-08-09)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.23.3 (2022-08-08)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.23.2 (2022-08-01)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.23.1 (2022-07-26)

* **Documentation**: Adds support for using RDS Proxies with RDS for MariaDB databases.

# v1.23.0 (2022-07-22)

* **Feature**: This release adds the "ModifyActivityStream" API with support for audit policy state locking and unlocking.

# v1.22.1 (2022-07-21)

* **Documentation**: Adds support for creating an RDS Proxy for an RDS for MariaDB database.

# v1.22.0 (2022-07-05)

* **Feature**: Adds waiters support for DBCluster.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.21.5 (2022-07-01)

* **Documentation**: Adds support for additional retention periods to Performance Insights.

# v1.21.4 (2022-06-29)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.21.3 (2022-06-07)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.21.2 (2022-05-17)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.21.1 (2022-05-06)

* **Documentation**: Various documentation improvements.

# v1.21.0 (2022-04-29)

* **Feature**: Feature - Adds support for Internet Protocol Version 6 (IPv6) on RDS database instances.

# v1.20.1 (2022-04-25)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.20.0 (2022-04-20)

* **Feature**: Added a new cluster-level attribute to set the capacity range for Aurora Serverless v2 instances.

# v1.19.0 (2022-04-15)

* **Feature**: Removes Amazon RDS on VMware with the deletion of APIs related to Custom Availability Zones and Media installation

# v1.18.4 (2022-03-30)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.3 (2022-03-24)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.2 (2022-03-23)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.18.1 (2022-03-15)

* **Documentation**: Various documentation improvements

# v1.18.0 (2022-03-08)

* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Documentation**: Updated service client model to latest release.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.17.0 (2022-02-24)

* **Feature**: API client updated
* **Feature**: Adds RetryMaxAttempts and RetryMod to API client Options. This allows the API clients' default Retryer to be configured from the shared configuration files or environment variables. Adding a new Retry mode of `Adaptive`. `Adaptive` retry mode is an experimental mode, adding client rate limiting when throttles reponses are received from an API. See [retry.AdaptiveMode](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/aws/retry#AdaptiveMode) for more details, and configuration options.
* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.16.0 (2022-01-14)

* **Feature**: Updated API models
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

# v1.9.1 (2021-10-11)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.9.0 (2021-09-17)

* **Feature**: Updated API client and endpoints to latest revision.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.8.0 (2021-08-27)

* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.7.1 (2021-08-19)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.7.0 (2021-08-04)

* **Feature**: Updated to latest API model.
* **Dependency Update**: Updated `github.com/aws/smithy-go` to latest version.
* **Dependency Update**: Updated to the latest SDK module versions

# v1.6.0 (2021-07-15)

* **Feature**: The ErrorCode method on generated service error types has been corrected to match the API model.
* **Dependency Update**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.5.0 (2021-06-25)

* **Feature**: API client updated
* **Feature**: Updated `github.com/aws/smithy-go` to latest version
* **Dependency Update**: Updated to the latest SDK module versions

# v1.4.2 (2021-06-11)

* **Documentation**: Updated to latest API model.

# v1.4.1 (2021-05-20)

* **Dependency Update**: Updated to the latest SDK module versions

# v1.4.0 (2021-05-14)

* **Feature**: Constant has been added to modules to enable runtime version inspection for reporting.
* **Dependency Update**: Updated to the latest SDK module versions

