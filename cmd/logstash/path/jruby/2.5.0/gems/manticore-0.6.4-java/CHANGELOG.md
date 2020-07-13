## v0.6

### v0.6.5

(unreleased)

### v0.6.4

* client_cert and client_key now take the literal keys as strings, OpenSSL::X509::Certificate/OpenSSL::PKey::Pkey instances, or key file paths. (#77)
* Reduced unnecessary string copying (!78 - thanks @kares)

### v0.6.2-v0.6.3

* Fixed the use of authentication information in proxy URLs (#71)
* Changed the default encoding to UTF-8 when a response MIME is application/json (#70)

### v0.6.1

* Manticore will accept a URI object (which it calls #to_s on) as an alternate to a String for the URL in client#get(url)

### v0.6.0

* Dependent jars are now vendored, but managed with jar-dependencies. This solves issues installing manticore on platforms that have out-of-date root certs, and simplifies the install process.
* Fixed timeout behaviors (#48 - thanks @hobodave)

## v0.5

### v0.5.5

* Marked Executor threads as daemon threads, so they won't impede host process shutdown

### v0.5.4

* Fixed a memory leak caused by at_exit hooks, which would cause allocated HttpClient resources to remain in scope even if a Client instance was collected

### v0.5.3

* Reduce the stale connection check default time to 2000ms for consistency with the HttpClient defaults

### v0.5.2

* Added Client#close to shut down the connection pool.

### v0.5.1

* Upgrade to HTTPClient and HTTPCore 4.5
* BREAKING CHANGE: Background request usage has changed. See [this commit](https://github.com/cheald/manticore/commit/174e2004d1865c201daf77494d50ab66527c12aa) for details.
* Client#async is now soft-deprecated in favor of Client#parallel or Client#batch, as the latter two more accurately reflect the intended usage. Client#background is for
  "don't block the calling thread"-style asynchronous calls.
* Manticore now uses jar-dependencies to install the HTTPComponents et al jars during gem installation, rather than shipping them in the gem
* DELETEs may now post entity bodies to allow spec-violate behavior similar to GETs (thanks @hobodave)

## v0.4

## v0.4.5 (pending, master branch)

* If you pass a block to background request creation, the background request is yielded before being queued on the executor. This allows you to attach
  on_success, etc handlers. This is a stopgap change that is backwards compatible with 0.4.x and will be changing in the 0.5.x release.

## v0.4.4

* Manticore now treats post bodies with binary encodings as binary byte lists rather than strings with an encoding
* Manticore now treats :params as :query for GET, HEAD, and DELETE requests, where :query is not specified, in order to minimize confusion.
* Deprecated dependency on the Addressable gem. URI building is now done with HTTPClient's utils package instead.
* Manticore no longer always sets a body and content-length for stubbed responses

### v0.4.3

* Manticore no longer automatically retries all request types. Only non-idempotent requests will be automatically retried by default.
* added the `:retry_non_idempotent` [bool] option, which instructs Manticore to automatically retry all request types, rather than just idempotent request types
* .pfx files are automatically recognized as PKCS12 stores
* Improved StubbedResponse's mimicry of Response
* Minor improvments to the Faraday adapter
* Added an option for eager auth, which instructs Manticore to present basic auth credentials on initial request, rather than being challenged for them. You should
  only use this if you have a specific need for it, as it may be a security concern otherwise.
* Manticore now cleans up the stale connection reaper thread at_exit. This may resolve memory leaks in servlet contexts.

### v0.4.2

* Fixed truststore documentation to be more clear (thanks @andrewvc)
* Always re-raise any errors thrown during request execution, not just a subset of expected exceptions (thanks @andrewvc)
* Add Connection: Keep-Alive to requests which indicate keepalive to ensure that HTTP/1.0 transactions honor keepalives. HTTP/1.1 requests should be unaffected.

### v0.4.1

* Add support for `ssl[:ca_file]`, `ssl[:client_cert]`, and `ssl[:client_key]`, to emulate OpenSSL features in other Ruby HTTP clients
* Integrate Faraday adapter for Manticore

### v0.4.0

* Proxy authentication is now supported
* Client#execute! no longer propagates exceptions; these should be handled in `on_failure`.
* Client#http and AsyncProxy now properly accept #delete
* Response#on_complete now receives the request as an argument

## v0.3

### v0.3.6

* GET requests may now accept bodies much like POST requests. Fixes interactions with Elasticsearch.

### v0.3.5

* Stubs now accept regexes for URLs in addition to strings (thanks @gmassanek)

### v0.3.4

* Fixed an issue that caused the presence of request-specific options (ie, max_redirects) to cause the request to use a
  default settings config, rather than respecting the client options. (thanks @zanker)
* Turn off connection state tracking by default; this enables connections to be shared across threads, and shouldn't be an
  issue for most installs. If you need it on, pass :ssl => {:track_state => true} when instantiating a client. (thanks @zanker)

### v0.3.3

* Update to HttpCommons 4.3.6
* Added Response#message (thanks @zanker)
* Fix issues with HTTP error messages that didn't contain a useful message
* Fixed an issue that would prevent the :protocols and :cipher_suites options from working

### v0.3.2
* :ignore_ssl_validation is now deprecated. It has been replaced with :ssl, which takes a hash of options. These include:

        :verify               - :strict (default), :browser, :none -- Specify hostname verification behaviors.
        :protocols            - An array of protocols to accept
        :cipher_suites        - An array of cipher suites to accept
        :truststore           - Path to a keytool trust store, for specifying custom trusted certificate signers
        :truststore_password  - Password for the file specified in `:truststore`
        :truststore_type      - Specify the trust store type (JKS, PKCS12)
        :keystore             - Path to a keytool trust store, for specifying client authentication certificates
        :keystore_password    - Password for the file specified in `:keystore`
        :keystore_type        - Specify the key store type (JKS, PKCS12)

  (thanks @torrancew)

* Fix encodings for bodies (thanks @synhaptein)

### v0.3.1
* Added `automatic_retries` (default 3) parameter to client. The client will automatically retry requests that failed
  due to socket exceptions and empty responses up to this number of times. The most practical effect of this setting is
  to automatically retry when the pool reuses a connection that a client unexpectedly closed.
* Added `request_timeout` to the RequestConfig used to construct requests.
* Fixed implementation of the `:query` parameter for GET, HEAD, and DELETE requests.

### v0.3.0

* Major refactor of `Response`/`AsyncResponse` to eliminate redundant code. `AsyncResponse` has been removed and
  its functionality has been rolled into `Response`.
* Added `StubbedResponse`, a subclass of `Response`, to be used for stubbing requests/responses for testing.
* Added `Client#stub`, `Client#unstub` and `Client#respond_with`
* Responses are now lazy-evaluated by default (similar to how `AsyncResponse` used to behave). The following
  rules apply:
  * Synchronous responses which do NOT pass a block are lazy-evaluated the first time one of their results is requested.
  * Synchronous responses which DO pass a block are evaluated immediately, and are passed to the handler block.
  * Async responses are always evaluted when `Client#execute!` is called.
* You can evaluate a `Response` at any time by invoking `#call` on it. Invoking an async response before `Client#execute`
  is called on it will cause `Client#execute` to throw an exception.
* Responses (both synchronous and async) may use on_success handlers and the like.

## v0.2
### v0.2.1

* Added basic auth support
* Added proxy support
* Added support for per-request cookies (as opposed to per-session cookies)
* Added a `Response#cookies` convenience method.

### v0.2.0

* Added documentation and licenses
* Significant performance overhaul
* Response handler blocks are now only yielded the Response. `#request` is available on
  the response object.
* Patched httpclient.jar to address https://issues.apache.org/jira/browse/HTTPCLIENT-1461
