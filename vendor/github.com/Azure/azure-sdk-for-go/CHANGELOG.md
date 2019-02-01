# CHANGELOG

-----

## `v6.0.0-beta`

| api                            | version            | note                               |
|:-------------------------------|:-------------------|:-----------------------------------|
| arm/authorization              | no change          | code refactoring                   |
| arm/batch                      | no change          | code refactoring                   |
| arm/compute                    | no change          | code refactoring                   |
| arm/containerservice           | 2016-03-30         | return                             |
| arm/datalake-analytics/account | 2015-10-01-preview | new                                |
| arm/datalake-store/filesystem  | no change          | moved to datalake-store/filesystem |
| arm/eventhub                   | no change          | code refactoring                   |
| arm/intune                     | no change          | code refactoring                   |
| arm/iothub                     | no change          | code refactoring                   |
| arm/keyvault                   | no change          | code refactoring                   |
| arm/mediaservices              | no change          | code refactoring                   |
| arm/network                    | no change          | code refactoring                   |
| arm/notificationhubs           | no change          | code refactoring                   |
| arm/redis                      | no change          | code refactoring                   |
| arm/resources/resources        | no change          | code refactoring                   |
| arm/resources/links            | 2016-09-01         | new                                |
| arm/resources/locks            | 2016-09-01         | updated                            |
| arm/resources/policy           | no change          | code refactoring                   |
| arm/resources/resources        | 2016-09-01         | updated                            |
| arm/servermanagement           | 2016-07-01-preview | updated                            |
| arm/web                        | no change          | code refactoring                   |

- storage: Added blob lease functionality and tests

## `v5.0.0-beta`

| api                           | version             | note             |
|:------------------------------|:--------------------|:-----------------|
| arm/network                   | 2016-09-01          | updated          |
| arm/servermanagement          | 2015-07-01-preview  | new              |
| arm/eventhub                  | 2015-08-01          | new              |
| arm/containerservice          | --                  | removed          |
| arm/resources/subscriptions   | no change           | code refactoring |
| arm/resources/features        | no change           | code refactoring |
| arm/resources/resources       | no change           | code refactoring |
| arm/datalake-store/accounts   | no change           | code refactoring |
| arm/datalake-store/filesystem | no change           | code refactoring |
| arm/notificationhubs          | no change           | code refactoring |
| arm/redis                     | no change           | code refactoring |

- storage: Add more file storage share operations.
- azure-rest-api-specs/commit/b8cdc2c50a0872fc0039f20c2b6b33aa0c2af4bf
- Uses go-autorest v7.2.1

## `v4.0.0-beta`

- arm/logic: breaking change in package logic.
- arm: parameter validation code added in all arm packages.
- Uses go-autorest v7.2.0.


## `v3.2.0-beta`

| api                         | version             | note      |
|:----------------------------|:--------------------|:----------|
| arm/mediaservices           | 2015-10-01          | new       |
| arm/keyvault                | 2015-06-01          | new       |
| arm/iothub                  | 2016-02-03          | new       |
| arm/datalake-store          | 2015-12-01          | new       |
| arm/network                 | 2016-06-01          | updated   |
| arm/resources/resources     | 2016-07-01          | updated   |
| arm/resources/policy        | 2016-04-01          | updated   |
| arm/servicebus              | 2015-08-01          | updated   |

- arm: uses go-autorest version v7.1.0.
- storage: fix for operating on blobs names containing special characters.
- storage: add SetBlobProperties(), update BlobProperties response fields.
- storage: make storage client work correctly with read-only secondary account.
- storage: add Azure Storage Emulator support.


## `v3.1.0-beta`

- Added a new arm/compute/containerservice (2016-03-30) package
- Reintroduced NewxxClientWithBaseURI method.
- Uses go-autorest version - v7.0.7.


## `v3.0.0-beta`

This release brings the Go SDK ARM packages up-to-date with Azure ARM Swagger files for most
services. Since the underlying [Swagger files](https://github.com/Azure/azure-rest-api-specs)
continue to change substantially, the ARM packages are still in *beta* status.

The ARM packages now align with the following API versions (*highlighted* packages are new or
updated in this release):

| api                         | version             | note      |
|:----------------------------|:--------------------|:----------|
| arm/authorization           | 2015-07-01          | no change |
| arm/intune                  | 2015-01-14-preview  | no change |
| arm/notificationhubs        | 2014-09-01          | no change |
| arm/resources/features      | 2015-12-01          | no change |
| arm/resources/subscriptions | 2015-11-01          | no change |
| arm/web                     | 2015-08-01          | no change |
| arm/cdn                     | 2016-04-02          | updated   |
| arm/compute                 | 2016-03-30          | updated   |
| arm/dns                     | 2016-04-01          | updated   |
| arm/logic                   | 2015-08-01-preview  | updated   |
| arm/network                 | 2016-03-30          | updated   |
| arm/redis                   | 2016-04-01          | updated   |
| arm/resources/resources     | 2016-02-01          | updated   |
| arm/resources/policy        | 2015-10-01-preview  | updated   |
| arm/resources/locks         | 2015-01-01          | updated (resources/authorization earlier)|
| arm/scheduler               | 2016-03-01          | updated   |
| arm/storage                 | 2016-01-01          | updated   |
| arm/search                  | 2015-02-28          | updated   |
| arm/batch                   | 2015-12-01          | new       |
| arm/cognitiveservices       | 2016-02-01-preview  | new       |
| arm/devtestlabs             | 2016-05-15          | new       |
| arm/machinelearning         | 2016-05-01-preview  | new       |
| arm/powerbiembedded         | 2016-01-29          | new       |
| arm/mobileengagement        | 2014-12-01          | new       |
| arm/servicebus              | 2014-09-01          | new       |
| arm/sql                     | 2015-05-01          | new       |
| arm/trafficmanager          | 2015-11-01          | new       |


Below are some design changes.
- Removed Api version from method arguments.
- Removed New...ClientWithBaseURI() method in all clients. BaseURI value is set in client.go.
- Uses go-autorest version v7.0.6.


## `v2.2.0-beta`

- Uses go-autorest version v7.0.5.
- Update version of pacakges "jwt-go" and "crypto" in glide.lock.


## `v2.1.1-beta`

- arm: Better error messages for long running operation failures (Uses go-autorest version v7.0.4).


## `v2.1.0-beta`

- arm: Uses go-autorest v7.0.3 (polling related updates).
- arm: Cancel channel argument added in long-running calls.
- storage: Allow caller to provide headers for DeleteBlob methods.
- storage: Enables connection sharing with http keepalive.
- storage: Add BlobPrefixes and Delimiter to BlobListResponse


## `v2.0.0-beta`

- Uses go-autorest v6.0.0 (Polling and Asynchronous requests related changes).

 
## `v0.5.0-beta`

Updated following packages to new API versions:
- arm/resources/features 2015-12-01
- arm/resources/resources 2015-11-01
- arm/resources/subscriptions 2015-11-01


### Changes 

 - SDK now uses go-autorest v3.0.0.



## `v0.4.0-beta`

This release brings the Go SDK ARM packages up-to-date with Azure ARM Swagger files for most
services. Since the underlying [Swagger files](https://github.com/Azure/azure-rest-api-specs)
continue to change substantially, the ARM packages are still in *beta* status.

The ARM packages now align with the following API versions (*highlighted* packages are new or
updated in this release):

- *arm/authorization 2015-07-01*
- *arm/cdn 2015-06-01*
- arm/compute 2015-06-15
- arm/dns 2015-05-04-preview
- *arm/intune 2015-01-14-preview*
- arm/logic 2015-02-01-preview
- *arm/network 2015-06-15*
- *arm/notificationhubs 2014-09-01*
- arm/redis 2015-08-01
- *arm/resources/authorization 2015-01-01*
- *arm/resources/features 2014-08-01-preview*
- *arm/resources/resources 2014-04-01-preview*
- *arm/resources/subscriptions 2014-04-01-preview*
- *arm/scheduler 2016-01-01*
- arm/storage 2015-06-15
- arm/web 2015-08-01

### Changes

- Moved the arm/authorization, arm/features, arm/resources, and arm/subscriptions packages under a new, resources, package (to reflect the corresponding Swagger structure)
- Added a new arm/authoriation (2015-07-01) package
- Added a new arm/cdn (2015-06-01) package
- Added a new arm/intune (2015-01-14-preview) package
- Udated arm/network (2015-06-01)
- Added a new arm/notificationhubs (2014-09-01) package
- Updated arm/scheduler (2016-01-01) package


-----

## `v0.3.0-beta`

- Corrected unintentional struct field renaming and client renaming in v0.2.0-beta

-----

## `v0.2.0-beta`

- Added support for DNS, Redis, and Web site services
- Updated Storage service to API version 2015-06-15
- Updated Network to include routing table support
- Address https://github.com/Azure/azure-sdk-for-go/issues/232
- Address https://github.com/Azure/azure-sdk-for-go/issues/231
- Address https://github.com/Azure/azure-sdk-for-go/issues/230
- Address https://github.com/Azure/azure-sdk-for-go/issues/224
- Address https://github.com/Azure/azure-sdk-for-go/issues/184
- Address https://github.com/Azure/azure-sdk-for-go/issues/183

------

## `v0.1.1-beta`

- Improves the UserAgent string to disambiguate arm packages from others in the SDK
- Improves setting the http.Response into generated results (reduces likelihood of a nil reference)
- Adds gofmt, golint, and govet to Travis CI for the arm packages

##### Fixed Issues

- https://github.com/Azure/azure-sdk-for-go/issues/196
- https://github.com/Azure/azure-sdk-for-go/issues/213

------

## v0.1.0-beta

This release addresses the issues raised against the alpha release and adds more features. Most
notably, to address the challenges of encoding JSON
(see the [comments](https://github.com/Azure/go-autorest#handling-empty-values) in the
[go-autorest](https://github.com/Azure/go-autorest) package) by using pointers for *all* structure
fields (with the exception of enumerations). The
[go-autorest/autorest/to](https://github.com/Azure/go-autorest/tree/master/autorest/to) package
provides helpers to convert to / from pointers. The examples demonstrate their usage.

Additionally, the packages now align with Go coding standards and pass both `golint` and `govet`.
Accomplishing this required renaming various fields and parameters (such as changing Url to URL).

##### Changes

- Changed request / response structures to use pointer fields.
- Changed methods to return `error` instead of `autorest.Error`.
- Re-divided methods to ease asynchronous requests.
- Added paged results support.
- Added a UserAgent string.
- Added changes necessary to pass golint and govet.
- Updated README.md with details on asynchronous requests and paging.
- Saved package dependencies through Godep (for the entire SDK).

##### Fixed Issues:

- https://github.com/Azure/azure-sdk-for-go/issues/205
- https://github.com/Azure/azure-sdk-for-go/issues/206
- https://github.com/Azure/azure-sdk-for-go/issues/211
- https://github.com/Azure/azure-sdk-for-go/issues/212

-----

## v0.1.0-alpha

This release introduces the Azure Resource Manager packages generated from the corresponding
[Swagger API](http://swagger.io) [definitions](https://github.com/Azure/azure-rest-api-specs).