# Changelog

All notable changes to this project will be documented in this file. For info on how to format all future additions to this file please reference [Keep A Changelog](https://keepachangelog.com/en/1.0.0/).

## [2.2.3] - 2020-02-11

- [CVE-2020-8184] Only decode cookie values

## [2.2.2] - 2020-02-11

### Fixed

- Fix incorrect `Rack::Request#host` value. ([#1591](https://github.com/rack/rack/pull/1591), [@ioquatix](https://github.com/ioquatix))
- Revert `Rack::Handler::Thin` implementation. ([#1583](https://github.com/rack/rack/pull/1583), [@jeremyevans](https://github.com/jeremyevans))
- Double assignment is still needed to prevent an "unused variable" warning. ([#1589](https://github.com/rack/rack/pull/1589), [@kamipo](https://github.com/kamipo))
- Fix to handle same_site option for session pool. ([#1587](https://github.com/rack/rack/pull/1587), [@kamipo](https://github.com/kamipo))

## [2.2.1] - 2020-02-09

### Fixed

- Rework `Rack::Request#ip` to handle empty `forwarded_for`. ([#1577](https://github.com/rack/rack/pull/1577), [@ioquatix](https://github.com/ioquatix))

## [2.2.0] - 2020-02-08

### SPEC Changes

- `rack.session` request environment entry must respond to `to_hash` and return unfrozen Hash. ([@jeremyevans](https://github.com/jeremyevans))
- Request environment cannot be frozen. ([@jeremyevans](https://github.com/jeremyevans))
- CGI values in the request environment with non-ASCII characters must use ASCII-8BIT encoding. ([@jeremyevans](https://github.com/jeremyevans))
- Improve SPEC/lint relating to SERVER_NAME, SERVER_PORT and HTTP_HOST. ([#1561](https://github.com/rack/rack/pull/1561), [@ioquatix](https://github.com/ioquatix))

### Added

- `rackup` supports multiple `-r` options and will require all arguments. ([@jeremyevans](https://github.com/jeremyevans))
- `Server` supports an array of paths to require for the `:require` option. ([@khotta](https://github.com/khotta))
- `Files` supports multipart range requests. ([@fatkodima](https://github.com/fatkodima))
- `Multipart::UploadedFile` supports an IO-like object instead of using the filesystem, using `:filename` and `:io` options. ([@jeremyevans](https://github.com/jeremyevans))
- `Multipart::UploadedFile` supports keyword arguments `:path`, `:content_type`, and `:binary` in addition to positional arguments. ([@jeremyevans](https://github.com/jeremyevans))
- `Static` supports a `:cascade` option for calling the app if there is no matching file. ([@jeremyevans](https://github.com/jeremyevans))
- `Session::Abstract::SessionHash#dig`. ([@jeremyevans](https://github.com/jeremyevans))
- `Response.[]` and `MockResponse.[]` for creating instances using status, headers, and body. ([@ioquatix](https://github.com/ioquatix))
- Convenient cache and content type methods for `Rack::Response`. ([#1555](https://github.com/rack/rack/pull/1555), [@ioquatix](https://github.com/ioquatix))

### Changed

- `Request#params` no longer rescues EOFError. ([@jeremyevans](https://github.com/jeremyevans))
- `Directory` uses a streaming approach, significantly improving time to first byte for large directories. ([@jeremyevans](https://github.com/jeremyevans))
- `Directory` no longer includes a Parent directory link in the root directory index. ([@jeremyevans](https://github.com/jeremyevans))
- `QueryParser#parse_nested_query` uses original backtrace when reraising exception with new class. ([@jeremyevans](https://github.com/jeremyevans))
- `ConditionalGet` follows RFC 7232 precedence if both If-None-Match and If-Modified-Since headers are provided. ([@jeremyevans](https://github.com/jeremyevans))
- `.ru` files supports the `frozen-string-literal` magic comment. ([@eregon](https://github.com/eregon))
- Rely on autoload to load constants instead of requiring internal files, make sure to require 'rack' and not just 'rack/...'. ([@jeremyevans](https://github.com/jeremyevans))
- `Etag` will continue sending ETag even if the response should not be cached. ([@henm](https://github.com/henm))
- `Request#host_with_port` no longer includes a colon for a missing or empty port. ([@AlexWayfer](https://github.com/AlexWayfer))
- All handlers uses keywords arguments instead of an options hash argument. ([@ioquatix](https://github.com/ioquatix))
- `Files` handling of range requests no longer return a body that supports `to_path`, to ensure range requests are handled correctly. ([@jeremyevans](https://github.com/jeremyevans))
- `Multipart::Generator` only includes `Content-Length` for files with paths, and `Content-Disposition` `filename` if the `UploadedFile` instance has one. ([@jeremyevans](https://github.com/jeremyevans))
- `Request#ssl?` is true for the `wss` scheme (secure websockets). ([@jeremyevans](https://github.com/jeremyevans))
- `Rack::HeaderHash` is memoized by default. ([#1549](https://github.com/rack/rack/pull/1549), [@ioquatix](https://github.com/ioquatix))
- `Rack::Directory` allow directory traversal inside root directory. ([#1417](https://github.com/rack/rack/pull/1417), [@ThomasSevestre](https://github.com/ThomasSevestre))
- Sort encodings by server preference. ([#1184](https://github.com/rack/rack/pull/1184), [@ioquatix](https://github.com/ioquatix), [@wjordan](https://github.com/wjordan))
- Rework host/hostname/authority implementation in `Rack::Request`. `#host` and `#host_with_port` have been changed to correctly return IPv6 addresses formatted with square brackets, as defined by [RFC3986](https://tools.ietf.org/html/rfc3986#section-3.2.2). ([#1561](https://github.com/rack/rack/pull/1561), [@ioquatix](https://github.com/ioquatix))
- `Rack::Builder` parsing options on first `#\` line is deprecated. ([#1574](https://github.com/rack/rack/pull/1574), [@ioquatix](https://github.com/ioquatix))

### Removed

- `Directory#path` as it was not used and always returned nil. ([@jeremyevans](https://github.com/jeremyevans))
- `BodyProxy#each` as it was only needed to work around a bug in Ruby <1.9.3. ([@jeremyevans](https://github.com/jeremyevans))
- `URLMap::INFINITY` and `URLMap::NEGATIVE_INFINITY`, in favor of `Float::INFINITY`. ([@ch1c0t](https://github.com/ch1c0t))
- Deprecation of `Rack::File`. It will be deprecated again in rack 2.2 or 3.0. ([@rafaelfranca](https://github.com/rafaelfranca))
- Support for Ruby 2.2 as it is well past EOL. ([@ioquatix](https://github.com/ioquatix))
- Remove `Rack::Files#response_body` as the implementation was broken. ([#1153](https://github.com/rack/rack/pull/1153), [@ioquatix](https://github.com/ioquatix))
- Remove `SERVER_ADDR` which was never part of the original SPEC. ([#1573](https://github.com/rack/rack/pull/1573), [@ioquatix](https://github.com/ioquatix))

### Fixed

- `Directory` correctly handles root paths containing glob metacharacters. ([@jeremyevans](https://github.com/jeremyevans))
- `Cascade` uses a new response object for each call if initialized with no apps. ([@jeremyevans](https://github.com/jeremyevans))
- `BodyProxy` correctly delegates keyword arguments to the body object on Ruby 2.7+. ([@jeremyevans](https://github.com/jeremyevans))
- `BodyProxy#method` correctly handles methods delegated to the body object. ([@jeremyevans](https://github.com/jeremyevans))
- `Request#host` and `Request#host_with_port` handle IPv6 addresses correctly. ([@AlexWayfer](https://github.com/AlexWayfer))
- `Lint` checks when response hijacking that `rack.hijack` is called with a valid object. ([@jeremyevans](https://github.com/jeremyevans))
- `Response#write` correctly updates `Content-Length` if initialized with a body. ([@jeremyevans](https://github.com/jeremyevans))
- `CommonLogger` includes `SCRIPT_NAME` when logging. ([@Erol](https://github.com/Erol))
- `Utils.parse_nested_query` correctly handles empty queries, using an empty instance of the params class instead of a hash. ([@jeremyevans](https://github.com/jeremyevans))
- `Directory` correctly escapes paths in links. ([@yous](https://github.com/yous))
- `Request#delete_cookie` and related `Utils` methods handle `:domain` and `:path` options in same call. ([@jeremyevans](https://github.com/jeremyevans))
- `Request#delete_cookie` and related `Utils` methods do an exact match on `:domain` and `:path` options. ([@jeremyevans](https://github.com/jeremyevans))
- `Static` no longer adds headers when a gzipped file request has a 304 response. ([@chooh](https://github.com/chooh))
- `ContentLength` sets `Content-Length` response header even for bodies not responding to `to_ary`. ([@jeremyevans](https://github.com/jeremyevans))
- Thin handler supports options passed directly to `Thin::Controllers::Controller`. ([@jeremyevans](https://github.com/jeremyevans))
- WEBrick handler no longer ignores `:BindAddress` option. ([@jeremyevans](https://github.com/jeremyevans))
- `ShowExceptions` handles invalid POST data. ([@jeremyevans](https://github.com/jeremyevans))
- Basic authentication requires a password, even if the password is empty. ([@jeremyevans](https://github.com/jeremyevans))
- `Lint` checks response is array with 3 elements, per SPEC. ([@jeremyevans](https://github.com/jeremyevans))
- Support for using `:SSLEnable` option when using WEBrick handler. (Gregor Melhorn)
- Close response body after buffering it when buffering. ([@ioquatix](https://github.com/ioquatix))
- Only accept `;` as delimiter when parsing cookies. ([@mrageh](https://github.com/mrageh))
- `Utils::HeaderHash#clear` clears the name mapping as well. ([@raxoft](https://github.com/raxoft)) 
- Support for passing `nil` `Rack::Files.new`, which notably fixes Rails' current `ActiveStorage::FileServer` implementation. ([@ioquatix](https://github.com/ioquatix))

### Documentation

- CHANGELOG updates. ([@aupajo](https://github.com/aupajo))
- Added [CONTRIBUTING](CONTRIBUTING.md). ([@dblock](https://github.com/dblock))

## [2.1.2] - 2020-01-27

- Fix multipart parser for some files to prevent denial of service ([@aiomaster](https://github.com/aiomaster))
- Fix `Rack::Builder#use` with keyword arguments ([@kamipo](https://github.com/kamipo))
- Skip deflating in Rack::Deflater if Content-Length is 0 ([@jeremyevans](https://github.com/jeremyevans))
- Remove `SessionHash#transform_keys`, no longer needed ([@pavel](https://github.com/pavel))
- Add to_hash to wrap Hash and Session classes ([@oleh-demyanyuk](https://github.com/oleh-demyanyuk))
- Handle case where session id key is requested but missing ([@jeremyevans](https://github.com/jeremyevans))

## [2.1.1] - 2020-01-12

- Remove `Rack::Chunked` from `Rack::Server` default middleware. ([#1475](https://github.com/rack/rack/pull/1475), [@ioquatix](https://github.com/ioquatix))
- Restore support for code relying on `SessionId#to_s`. ([@jeremyevans](https://github.com/jeremyevans))

## [2.1.0] - 2020-01-10

### Added

- Add support for `SameSite=None` cookie value. ([@hennikul](https://github.com/hennikul))
- Add trailer headers. ([@eileencodes](https://github.com/eileencodes))
- Add MIME Types for video streaming. ([@styd](https://github.com/styd))
- Add MIME Type for WASM. ([@buildrtech](https://github.com/buildrtech))
- Add `Early Hints(103)` to status codes. ([@egtra](https://github.com/egtra))
- Add `Too Early(425)` to status codes. ([@y-yagi]((https://github.com/y-yagi)))
- Add `Bandwidth Limit Exceeded(509)` to status codes. ([@CJKinni](https://github.com/CJKinni))
- Add method for custom `ip_filter`. ([@svcastaneda](https://github.com/svcastaneda))
- Add boot-time profiling capabilities to `rackup`. ([@tenderlove](https://github.com/tenderlove))
- Add multi mapping support for `X-Accel-Mappings` header. ([@yoshuki](https://github.com/yoshuki))
- Add `sync: false` option to `Rack::Deflater`. (Eric Wong)
- Add `Builder#freeze_app` to freeze application and all middleware instances. ([@jeremyevans](https://github.com/jeremyevans))
- Add API to extract cookies from `Rack::MockResponse`. ([@petercline](https://github.com/petercline))

### Changed

- Don't propagate nil values from middleware. ([@ioquatix](https://github.com/ioquatix))
- Lazily initialize the response body and only buffer it if required. ([@ioquatix](https://github.com/ioquatix))
- Fix deflater zlib buffer errors on empty body part. ([@felixbuenemann](https://github.com/felixbuenemann))
- Set `X-Accel-Redirect` to percent-encoded path. ([@diskkid](https://github.com/diskkid))
- Remove unnecessary buffer growing when parsing multipart. ([@tainoe](https://github.com/tainoe))
- Expand the root path in `Rack::Static` upon initialization. ([@rosenfeld](https://github.com/rosenfeld))
- Make `ShowExceptions` work with binary data. ([@axyjo](https://github.com/axyjo))
- Use buffer string when parsing multipart requests. ([@janko-m](https://github.com/janko-m))
- Support optional UTF-8 Byte Order Mark (BOM) in config.ru. ([@mikegee](https://github.com/mikegee))
- Handle `X-Forwarded-For` with optional port. ([@dpritchett](https://github.com/dpritchett))
- Use `Time#httpdate` format for Expires, as proposed by RFC 7231. ([@nanaya](https://github.com/nanaya))
- Make `Utils.status_code` raise an error when the status symbol is invalid instead of `500`. ([@adambutler](https://github.com/adambutler))
- Rename `Request::SCHEME_WHITELIST` to `Request::ALLOWED_SCHEMES`.
- Make `Multipart::Parser.get_filename` accept files with `+` in their name. ([@lucaskanashiro](https://github.com/lucaskanashiro))
- Add Falcon to the default handler fallbacks. ([@ioquatix](https://github.com/ioquatix))
- Update codebase to avoid string mutations in preparation for `frozen_string_literals`. ([@pat](https://github.com/pat))
- Change `MockRequest#env_for` to rely on the input optionally responding to `#size` instead of `#length`. ([@janko](https://github.com/janko))
- Rename `Rack::File` -> `Rack::Files` and add deprecation notice. ([@postmodern](https://github.com/postmodern)).
- Prefer Base64 “strict encoding” for Base64 cookies. ([@ioquatix](https://github.com/ioquatix))

### Removed

- Remove `to_ary` from Response ([@tenderlove](https://github.com/tenderlove))
- Deprecate `Rack::Session::Memcache` in favor of `Rack::Session::Dalli` from dalli gem ([@fatkodima](https://github.com/fatkodima))

### Fixed

- Eliminate warnings for Ruby 2.7. ([@osamtimizer](https://github.com/osamtimizer]))

### Documentation

- Update broken example in `Session::Abstract::ID` documentation. ([tonytonyjan](https://github.com/tonytonyjan))
- Add Padrino to the list of frameworks implementing Rack. ([@wikimatze](https://github.com/wikimatze))
- Remove Mongrel from the suggested server options in the help output. ([@tricknotes](https://github.com/tricknotes))
- Replace `HISTORY.md` and `NEWS.md` with `CHANGELOG.md`. ([@twitnithegirl](https://github.com/twitnithegirl))
- CHANGELOG updates. ([@drenmi](https://github.com/Drenmi), [@p8](https://github.com/p8))

## [2.0.8] - 2019-12-08

### Security

- [[CVE-2019-16782](https://nvd.nist.gov/vuln/detail/CVE-2019-16782)] Prevent timing attacks targeted at session ID lookup. BREAKING CHANGE: Session ID is now a SessionId instance instead of a String. ([@tenderlove](https://github.com/tenderlove), [@rafaelfranca](https://github.com/rafaelfranca))

## [1.6.12] - 2019-12-08

### Security

- [[CVE-2019-16782](https://nvd.nist.gov/vuln/detail/CVE-2019-16782)] Prevent timing attacks targeted at session ID lookup. BREAKING CHANGE: Session ID is now a SessionId instance instead of a String. ([@tenderlove](https://github.com/tenderlove), [@rafaelfranca](https://github.com/rafaelfranca))

## [2.0.7] - 2019-04-02

### Fixed

- Remove calls to `#eof?` on Rack input in `Multipart::Parser`, as this breaks the specification. ([@matthewd](https://github.com/matthewd))
- Preserve forwarded IP addresses for trusted proxy chains. ([@SamSaffron](https://github.com/SamSaffron))

## [2.0.6] - 2018-11-05

### Fixed

- [[CVE-2018-16470](https://nvd.nist.gov/vuln/detail/CVE-2018-16470)] Reduce buffer size of `Multipart::Parser` to avoid pathological parsing. ([@tenderlove](https://github.com/tenderlove))
- Fix a call to a non-existing method `#accepts_html` in the `ShowExceptions` middleware. ([@tomelm](https://github.com/tomelm))
- [[CVE-2018-16471](https://nvd.nist.gov/vuln/detail/CVE-2018-16471)] Whitelist HTTP and HTTPS schemes in `Request#scheme` to prevent a possible XSS attack. ([@PatrickTulskie](https://github.com/PatrickTulskie))

## [2.0.5] - 2018-04-23

### Fixed

- Record errors originating from invalid UTF8 in `MethodOverride` middleware instead of breaking. ([@mclark](https://github.com/mclark))

## [2.0.4] - 2018-01-31

### Changed

- Ensure the `Lock` middleware passes the original `env` object. ([@lugray](https://github.com/lugray))
- Improve performance of `Multipart::Parser` when uploading large files. ([@tompng](https://github.com/tompng))
- Increase buffer size in `Multipart::Parser` for better performance. ([@jkowens](https://github.com/jkowens))
- Reduce memory usage of `Multipart::Parser` when uploading large files. ([@tompng](https://github.com/tompng))
- Replace ConcurrentRuby dependency with native `Queue`. ([@devmchakan](https://github.com/devmchakan))

### Fixed

- Require the correct digest algorithm in the `ETag` middleware. ([@matthewd](https://github.com/matthewd))

### Documentation

- Update homepage links to use SSL. ([@hugoabonizio](https://github.com/hugoabonizio))

## [2.0.3] - 2017-05-15

### Changed

- Ensure `env` values are ASCII 8-bit encoded. ([@eileencodes](https://github.com/eileencodes))

### Fixed

- Prevent exceptions when a class with mixins inherits from `Session::Abstract::ID`. ([@jnraine](https://github.com/jnraine))

## [2.0.2] - 2017-05-08

### Added

- Allow `Session::Abstract::SessionHash#fetch` to accept a block with a default value. ([@yannvanhalewyn](https://github.com/yannvanhalewyn))
- Add `Builder#freeze_app` to freeze application and all middleware. ([@jeremyevans](https://github.com/jeremyevans))

### Changed

- Freeze default session options to avoid accidental mutation. ([@kirs](https://github.com/kirs))
- Detect partial hijack without hash headers. ([@devmchakan](https://github.com/devmchakan))
- Update tests to use MiniTest 6 matchers. ([@tonytonyjan](https://github.com/tonytonyjan))
- Allow 205 Reset Content responses to set a Content-Length, as RFC 7231 proposes setting this to 0. ([@devmchakan](https://github.com/devmchakan))

### Fixed

- Handle `NULL` bytes in multipart filenames. ([@casperisfine](https://github.com/casperisfine))
- Remove warnings due to miscapitalized global. ([@ioquatix](https://github.com/ioquatix))
- Prevent exceptions caused by a race condition on multi-threaded servers. ([@sophiedeziel](https://github.com/sophiedeziel))
- Add RDoc as an explicit depencency for `doc` group. ([@tonytonyjan](https://github.com/tonytonyjan))
- Record errors originating from `Multipart::Parser` in the `MethodOverride` middleware instead of letting them bubble up. ([@carlzulauf](https://github.com/carlzulauf))
- Remove remaining use of removed `Utils#bytesize` method from the `File` middleware. ([@brauliomartinezlm](https://github.com/brauliomartinezlm))

### Removed

- Remove `deflate` encoding support to reduce caching overhead. ([@devmchakan](https://github.com/devmchakan))

### Documentation

- Update broken example in `Deflater` documentation. ([@mwpastore](https://github.com/mwpastore))

## [2.0.1] - 2016-06-30

### Changed

- Remove JSON as an explicit dependency. ([@mperham](https://github.com/mperham))


# History/News Archive
Items below this line are from the previously maintained HISTORY.md and NEWS.md files.

## [2.0.0.rc1] 2016-05-06
- Rack::Session::Abstract::ID is deprecated. Please change to use Rack::Session::Abstract::Persisted

## [2.0.0.alpha] 2015-12-04
- First-party "SameSite" cookies. Browsers omit SameSite cookies from third-party requests, closing the door on many CSRF attacks.
- Pass `same_site: true` (or `:strict`) to enable: response.set_cookie 'foo', value: 'bar', same_site: true or `same_site: :lax` to use Lax enforcement: response.set_cookie 'foo', value: 'bar', same_site: :lax
- Based on version 7 of the Same-site Cookies internet draft:
	https://tools.ietf.org/html/draft-west-first-party-cookies-07
- Thanks to Ben Toews (@mastahyeti) and Bob Long (@bobjflong) for updating to drafts 5 and 7.
- Add `Rack::Events` middleware for adding event based middleware: middleware that does not care about the response body, but only cares about doing work at particular points in the request / response lifecycle.
- Add `Rack::Request#authority` to calculate the authority under which the response is being made (this will be handy for h2 pushes).
- Add `Rack::Response::Helpers#cache_control` and `cache_control=`. Use this for setting cache control headers on your response objects.
- Add `Rack::Response::Helpers#etag` and `etag=`.  Use this for setting etag values on the response.
- Introduce `Rack::Response::Helpers#add_header` to add a value to a multi-valued response header. Implemented in terms of other `Response#*_header` methods, so it's available to any response-like class that includes the `Helpers` module.
- Add `Rack::Request#add_header` to match.
- `Rack::Session::Abstract::ID` IS DEPRECATED.  Please switch to `Rack::Session::Abstract::Persisted`. `Rack::Session::Abstract::Persisted` uses a request object rather than the `env` hash.
- Pull `ENV` access inside the request object in to a module.  This will help with legacy Request objects that are ENV based but don't want to inherit from Rack::Request
- Move most methods on the `Rack::Request` to a module `Rack::Request::Helpers` and use public API to get values from the request object.  This enables users to mix `Rack::Request::Helpers` in to their own objects so they can implement `(get|set|fetch|each)_header` as they see fit (for example a proxy object).
- Files and directories with + in the name are served correctly. Rather than unescaping paths like a form, we unescape with a URI parser using `Rack::Utils.unescape_path`. Fixes #265
- Tempfiles are automatically closed in the case that there were too
	many posted.
- Added methods for manipulating response headers that don't assume
	they're stored as a Hash. Response-like classes may include the
	Rack::Response::Helpers module if they define these methods:
    - Rack::Response#has_header?
	- Rack::Response#get_header
	- Rack::Response#set_header
	- Rack::Response#delete_header
- Introduce Util.get_byte_ranges that will parse the value of the HTTP_RANGE string passed to it without depending on the `env` hash. `byte_ranges` is deprecated in favor of this method.
- Change Session internals to use Request objects for looking up session information. This allows us to only allocate one request object when dealing with session objects (rather than doing it every time we need to manipulate cookies, etc).
- Add `Rack::Request#initialize_copy` so that the env is duped when the request gets duped.
- Added methods for manipulating request specific data.  This includes
	data set as CGI parameters, and just any arbitrary data the user wants
	to associate with a particular request.  New methods:
	 - Rack::Request#has_header?
	 - Rack::Request#get_header
	 - Rack::Request#fetch_header
	 - Rack::Request#each_header
	 - Rack::Request#set_header
	 - Rack::Request#delete_header
- lib/rack/utils.rb: add a method for constructing "delete" cookie
	headers.  This allows us to construct cookie headers without depending
	on the side effects of mutating a hash.
- Prevent extremely deep parameters from being parsed. CVE-2015-3225

## [1.6.1] 2015-05-06
  - Fix CVE-2014-9490, denial of service attack in OkJson
  - Use a monotonic time for Rack::Runtime, if available
  - RACK_MULTIPART_LIMIT changed to RACK_MULTIPART_PART_LIMIT (RACK_MULTIPART_LIMIT is deprecated and will be removed in 1.7.0)

## [1.5.3] 2015-05-06
  - Fix CVE-2014-9490, denial of service attack in OkJson
  - Backport bug fixes to 1.5 series

## [1.6.0] 2014-01-18
  - Response#unauthorized? helper
  - Deflater now accepts an options hash to control compression on a per-request level
  - Builder#warmup method for app preloading
  - Request#accept_language method to extract HTTP_ACCEPT_LANGUAGE
  - Add quiet mode of rack server, rackup --quiet
  - Update HTTP Status Codes to RFC 7231
  - Less strict header name validation according to RFC 2616
  - SPEC updated to specify headers conform to RFC7230 specification
  - Etag correctly marks etags as weak
  - Request#port supports multiple x-http-forwarded-proto values
  - Utils#multipart_part_limit configures the maximum number of parts a request can contain
  - Default host to localhost when in development mode
  - Various bugfixes and performance improvements

## [1.5.2] 2013-02-07
  - Fix CVE-2013-0263, timing attack against Rack::Session::Cookie
  - Fix CVE-2013-0262, symlink path traversal in Rack::File
  - Add various methods to Session for enhanced Rails compatibility
  - Request#trusted_proxy? now only matches whole strings
  - Add JSON cookie coder, to be default in Rack 1.6+ due to security concerns
  - URLMap host matching in environments that don't set the Host header fixed
  - Fix a race condition that could result in overwritten pidfiles
  - Various documentation additions

## [1.4.5] 2013-02-07
  - Fix CVE-2013-0263, timing attack against Rack::Session::Cookie
  - Fix CVE-2013-0262, symlink path traversal in Rack::File

## [1.1.6, 1.2.8, 1.3.10] 2013-02-07
  - Fix CVE-2013-0263, timing attack against Rack::Session::Cookie

## [1.5.1] 2013-01-28
  - Rack::Lint check_hijack now conforms to other parts of SPEC
  - Added hash-like methods to Abstract::ID::SessionHash for compatibility
  - Various documentation corrections

## [1.5.0] 2013-01-21
  - Introduced hijack SPEC, for before-response and after-response hijacking
  - SessionHash is no longer a Hash subclass
  - Rack::File cache_control parameter is removed, in place of headers options
  - Rack::Auth::AbstractRequest#scheme now yields strings, not symbols
  - Rack::Utils cookie functions now format expires in RFC 2822 format
  - Rack::File now has a default mime type
  - rackup -b 'run Rack::Files.new(".")', option provides command line configs
  - Rack::Deflater will no longer double encode bodies
  - Rack::Mime#match? provides convenience for Accept header matching
  - Rack::Utils#q_values provides splitting for Accept headers
  - Rack::Utils#best_q_match provides a helper for Accept headers
  - Rack::Handler.pick provides convenience for finding available servers
  - Puma added to the list of default servers (preferred over Webrick)
  - Various middleware now correctly close body when replacing it
  - Rack::Request#params is no longer persistent with only GET params
  - Rack::Request#update_param and #delete_param provide persistent operations
  - Rack::Request#trusted_proxy? now returns true for local unix sockets
  - Rack::Response no longer forces Content-Types
  - Rack::Sendfile provides local mapping configuration options
  - Rack::Utils#rfc2109 provides old netscape style time output
  - Updated HTTP status codes
  - Ruby 1.8.6 likely no longer passes tests, and is no longer fully supported

## [1.4.4, 1.3.9, 1.2.7, 1.1.5] 2013-01-13
  - [SEC] Rack::Auth::AbstractRequest no longer symbolizes arbitrary strings
  - Fixed erroneous test case in the 1.3.x series

## [1.4.3] 2013-01-07
  - Security: Prevent unbounded reads in large multipart boundaries

## [1.3.8] 2013-01-07
  - Security: Prevent unbounded reads in large multipart boundaries

## [1.4.2] 2013-01-06
  - Add warnings when users do not provide a session secret
  - Fix parsing performance for unquoted filenames
  - Updated URI backports
  - Fix URI backport version matching, and silence constant warnings
  - Correct parameter parsing with empty values
  - Correct rackup '-I' flag, to allow multiple uses
  - Correct rackup pidfile handling
  - Report rackup line numbers correctly
  - Fix request loops caused by non-stale nonces with time limits
  - Fix reloader on Windows
  - Prevent infinite recursions from Response#to_ary
  - Various middleware better conforms to the body close specification
  - Updated language for the body close specification
  - Additional notes regarding ECMA escape compatibility issues
  - Fix the parsing of multiple ranges in range headers
  - Prevent errors from empty parameter keys
  - Added PATCH verb to Rack::Request
  - Various documentation updates
  - Fix session merge semantics (fixes rack-test)
  - Rack::Static :index can now handle multiple directories
  - All tests now utilize Rack::Lint (special thanks to Lars Gierth)
  - Rack::File cache_control parameter is now deprecated, and removed by 1.5
  - Correct Rack::Directory script name escaping
  - Rack::Static supports header rules for sophisticated configurations
  - Multipart parsing now works without a Content-Length header
  - New logos courtesy of Zachary Scott!
  - Rack::BodyProxy now explicitly defines #each, useful for C extensions
  - Cookies that are not URI escaped no longer cause exceptions

## [1.3.7] 2013-01-06
  - Add warnings when users do not provide a session secret
  - Fix parsing performance for unquoted filenames
  - Updated URI backports
  - Fix URI backport version matching, and silence constant warnings
  - Correct parameter parsing with empty values
  - Correct rackup '-I' flag, to allow multiple uses
  - Correct rackup pidfile handling
  - Report rackup line numbers correctly
  - Fix request loops caused by non-stale nonces with time limits
  - Fix reloader on Windows
  - Prevent infinite recursions from Response#to_ary
  - Various middleware better conforms to the body close specification
  - Updated language for the body close specification
  - Additional notes regarding ECMA escape compatibility issues
  - Fix the parsing of multiple ranges in range headers

## [1.2.6] 2013-01-06
  - Add warnings when users do not provide a session secret
  - Fix parsing performance for unquoted filenames

## [1.1.4] 2013-01-06
  - Add warnings when users do not provide a session secret

## [1.4.1] 2012-01-22
  - Alter the keyspace limit calculations to reduce issues with nested params
  - Add a workaround for multipart parsing where files contain unescaped "%"
  - Added Rack::Response::Helpers#method_not_allowed? (code 405)
  - Rack::File now returns 404 for illegal directory traversals
  - Rack::File now returns 405 for illegal methods (non HEAD/GET)
  - Rack::Cascade now catches 405 by default, as well as 404
  - Cookies missing '--' no longer cause an exception to be raised
  - Various style changes and documentation spelling errors
  - Rack::BodyProxy always ensures to execute its block
  - Additional test coverage around cookies and secrets
  - Rack::Session::Cookie can now be supplied either secret or old_secret
  - Tests are no longer dependent on set order
  - Rack::Static no longer defaults to serving index files
  - Rack.release was fixed

## [1.4.0] 2011-12-28
  - Ruby 1.8.6 support has officially been dropped. Not all tests pass.
  - Raise sane error messages for broken config.ru
  - Allow combining run and map in a config.ru
  - Rack::ContentType will not set Content-Type for responses without a body
  - Status code 205 does not send a response body
  - Rack::Response::Helpers will not rely on instance variables
  - Rack::Utils.build_query no longer outputs '=' for nil query values
  - Various mime types added
  - Rack::MockRequest now supports HEAD
  - Rack::Directory now supports files that contain RFC3986 reserved chars
  - Rack::File now only supports GET and HEAD requests
  - Rack::Server#start now passes the block to Rack::Handler::<h>#run
  - Rack::Static now supports an index option
  - Added the Teapot status code
  - rackup now defaults to Thin instead of Mongrel (if installed)
  - Support added for HTTP_X_FORWARDED_SCHEME
  - Numerous bug fixes, including many fixes for new and alternate rubies

## [1.1.3] 2011-12-28
  - Security fix. http://www.ocert.org/advisories/ocert-2011-003.html
    Further information here: http://jruby.org/2011/12/27/jruby-1-6-5-1

## [1.3.5] 2011-10-17
  - Fix annoying warnings caused by the backport in 1.3.4

## [1.3.4] 2011-10-01
  - Backport security fix from 1.9.3, also fixes some roundtrip issues in URI
  - Small documentation update
  - Fix an issue where BodyProxy could cause an infinite recursion
  - Add some supporting files for travis-ci

## [1.2.4] 2011-09-16
  - Fix a bug with MRI regex engine to prevent XSS by malformed unicode

## [1.3.3] 2011-09-16
  - Fix bug with broken query parameters in Rack::ShowExceptions
  - Rack::Request#cookies no longer swallows exceptions on broken input
  - Prevents XSS attacks enabled by bug in Ruby 1.8's regexp engine
  - Rack::ConditionalGet handles broken If-Modified-Since helpers

## [1.3.2] 2011-07-16
  - Fix for Rails and rack-test, Rack::Utils#escape calls to_s

## [1.3.1] 2011-07-13
  - Fix 1.9.1 support
  - Fix JRuby support
  - Properly handle $KCODE in Rack::Utils.escape
  - Make method_missing/respond_to behavior consistent for Rack::Lock,
    Rack::Auth::Digest::Request and Rack::Multipart::UploadedFile
  - Reenable passing rack.session to session middleware
  - Rack::CommonLogger handles streaming responses correctly
  - Rack::MockResponse calls close on the body object
  - Fix a DOS vector from MRI stdlib backport

## [1.2.3] 2011-05-22
  - Pulled in relevant bug fixes from 1.3
  - Fixed 1.8.6 support

## [1.3.0] 2011-05-22
  - Various performance optimizations
  - Various multipart fixes
  - Various multipart refactors
  - Infinite loop fix for multipart
  - Test coverage for Rack::Server returns
  - Allow files with '..', but not path components that are '..'
  - rackup accepts handler-specific options on the command line
  - Request#params no longer merges POST into GET (but returns the same)
  - Use URI.encode_www_form_component instead. Use core methods for escaping.
  - Allow multi-line comments in the config file
  - Bug L#94 reported by Nikolai Lugovoi, query parameter unescaping.
  - Rack::Response now deletes Content-Length when appropriate
  - Rack::Deflater now supports streaming
  - Improved Rack::Handler loading and searching
  - Support for the PATCH verb
  - env['rack.session.options'] now contains session options
  - Cookies respect renew
  - Session middleware uses SecureRandom.hex

## [1.2.2, 1.1.2] 2011-03-13
  - Security fix in Rack::Auth::Digest::MD5: when authenticator
    returned nil, permission was granted on empty password.

## [1.2.1] 2010-06-15
  - Make CGI handler rewindable
  - Rename spec/ to test/ to not conflict with SPEC on lesser
    operating systems

## [1.2.0] 2010-06-13
  - Removed Camping adapter: Camping 2.0 supports Rack as-is
  - Removed parsing of quoted values
  - Add Request.trace? and Request.options?
  - Add mime-type for .webm and .htc
  - Fix HTTP_X_FORWARDED_FOR
  - Various multipart fixes
  - Switch test suite to bacon

## [1.1.0] 2010-01-03
  - Moved Auth::OpenID to rack-contrib.
  - SPEC change that relaxes Lint slightly to allow subclasses of the
    required types
  - SPEC change to document rack.input binary mode in greator detail
  - SPEC define optional rack.logger specification
  - File servers support X-Cascade header
  - Imported Config middleware
  - Imported ETag middleware
  - Imported Runtime middleware
  - Imported Sendfile middleware
  - New Logger and NullLogger middlewares
  - Added mime type for .ogv and .manifest.
  - Don't squeeze PATH_INFO slashes
  - Use Content-Type to determine POST params parsing
  - Update Rack::Utils::HTTP_STATUS_CODES hash
  - Add status code lookup utility
  - Response should call #to_i on the status
  - Add Request#user_agent
  - Request#host knows about forwarded host
  - Return an empty string for Request#host if HTTP_HOST and
    SERVER_NAME are both missing
  - Allow MockRequest to accept hash params
  - Optimizations to HeaderHash
  - Refactored rackup into Rack::Server
  - Added Utils.build_nested_query to complement Utils.parse_nested_query
  - Added Utils::Multipart.build_multipart to complement
    Utils::Multipart.parse_multipart
  - Extracted set and delete cookie helpers into Utils so they can be
    used outside Response
  - Extract parse_query and parse_multipart in Request so subclasses
    can change their behavior
  - Enforce binary encoding in RewindableInput
  - Set correct external_encoding for handlers that don't use RewindableInput

## [1.0.1] 2009-10-18
  - Bump remainder of rack.versions.
  - Support the pure Ruby FCGI implementation.
  - Fix for form names containing "=": split first then unescape components
  - Fixes the handling of the filename parameter with semicolons in names.
  - Add anchor to nested params parsing regexp to prevent stack overflows
  - Use more compatible gzip write api instead of "<<".
  - Make sure that Reloader doesn't break when executed via ruby -e
  - Make sure WEBrick respects the :Host option
  - Many Ruby 1.9 fixes.

## [1.0.0] 2009-04-25
  - SPEC change: Rack::VERSION has been pushed to [1,0].
  - SPEC change: header values must be Strings now, split on "\n".
  - SPEC change: Content-Length can be missing, in this case chunked transfer
    encoding is used.
  - SPEC change: rack.input must be rewindable and support reading into
    a buffer, wrap with Rack::RewindableInput if it isn't.
  - SPEC change: rack.session is now specified.
  - SPEC change: Bodies can now additionally respond to #to_path with
    a filename to be served.
  - NOTE: String bodies break in 1.9, use an Array consisting of a
    single String instead.
  - New middleware Rack::Lock.
  - New middleware Rack::ContentType.
  - Rack::Reloader has been rewritten.
  - Major update to Rack::Auth::OpenID.
  - Support for nested parameter parsing in Rack::Response.
  - Support for redirects in Rack::Response.
  - HttpOnly cookie support in Rack::Response.
  - The Rakefile has been rewritten.
  - Many bugfixes and small improvements.

## [0.9.1] 2009-01-09
  - Fix directory traversal exploits in Rack::File and Rack::Directory.

## [0.9] 2009-01-06
  - Rack is now managed by the Rack Core Team.
  - Rack::Lint is stricter and follows the HTTP RFCs more closely.
  - Added ConditionalGet middleware.
  - Added ContentLength middleware.
  - Added Deflater middleware.
  - Added Head middleware.
  - Added MethodOverride middleware.
  - Rack::Mime now provides popular MIME-types and their extension.
  - Mongrel Header now streams.
  - Added Thin handler.
  - Official support for swiftiplied Mongrel.
  - Secure cookies.
  - Made HeaderHash case-preserving.
  - Many bugfixes and small improvements.

## [0.4] 2008-08-21
  - New middleware, Rack::Deflater, by Christoffer Sawicki.
  - OpenID authentication now needs ruby-openid 2.
  - New Memcache sessions, by blink.
  - Explicit EventedMongrel handler, by Joshua Peek <josh@joshpeek.com>
  - Rack::Reloader is not loaded in rackup development mode.
  - rackup can daemonize with -D.
  - Many bugfixes, especially for pool sessions, URLMap, thread safety
    and tempfile handling.
  - Improved tests.
  - Rack moved to Git.

## [0.3] 2008-02-26
  - LiteSpeed handler, by Adrian Madrid.
  - SCGI handler, by Jeremy Evans.
  - Pool sessions, by blink.
  - OpenID authentication, by blink.
  - :Port and :File options for opening FastCGI sockets, by blink.
  - Last-Modified HTTP header for Rack::File, by blink.
  - Rack::Builder#use now accepts blocks, by Corey Jewett.
    (See example/protectedlobster.ru)
  - HTTP status 201 can contain a Content-Type and a body now.
  - Many bugfixes, especially related to Cookie handling.

## [0.2] 2007-05-16
  - HTTP Basic authentication.
  - Cookie Sessions.
  - Static file handler.
  - Improved Rack::Request.
  - Improved Rack::Response.
  - Added Rack::ShowStatus, for better default error messages.
  - Bug fixes in the Camping adapter.
  - Removed Rails adapter, was too alpha.

## [0.1] 2007-03-03
