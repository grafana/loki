/*
Package endpointdiscovery provides a feature implemented in the AWS SDK for Go V2 that
allows client to fetch a valid endpoint to serve an API request. Discovered
endpoints are stored in an internal thread-safe cache to reduce the number
of calls made to fetch the endpoint.

Endpoint discovery stores endpoint by associating to a generated cache key.
Cache key is built using service-modeled sdkId and any service-defined input
identifiers provided by the customer.

Endpoint cache keys follow the grammar:

	key = sdkId.identifiers

	identifiers = map[string]string

The endpoint discovery cache implementation is internal. Clients resolves the
cache size to 10 entries. Each entry may contain multiple host addresses as
returned by the service.

Each discovered endpoint has a TTL associated to it, and are evicted from
cache lazily i.e. when client tries to retrieve an endpoint but finds an
expired entry instead.

Endpoint discovery feature can be turned on by setting the
`AWS_ENABLE_ENDPOINT_DISCOVERY` env variable to TRUE.

By default, the feature is set to AUTO - indicating operations that require
endpoint discovery always use it. To completely turn off the feature, one
should set the value as FALSE. Similar configuration rules apply for shared
config file where key is `endpoint_discovery_enabled`.
*/
package endpointdiscovery
