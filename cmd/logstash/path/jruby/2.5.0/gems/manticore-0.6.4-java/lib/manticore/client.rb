require "thread"
require "base64"
require "weakref"
require "openssl_pkcs8_pure"

module Manticore
  # @!macro [new] http_method_shared
  #   @param  url [String] URL to request
  #   @param  options [Hash]
  #   @option options [Hash]     query              Hash of options to be added to the URL as part of the query string
  #   @option options [Hash]     params             Hash of options to pass as a request body. For GET, HEAD, and DELETE requests,
  #                                                   :params will be treated as :query if :query is not present.
  #   @option options [Hash]     headers            Hash of options to pass as additional request headers
  #   @option options [String]   proxy              Proxy host in form: http://proxy.org:1234
  #   @option options [Hash]     proxy              Proxy host in form: {host: 'proxy.org'[, port: 80[, scheme: 'http']]}
  #   @option options [URI]      proxy              Proxy host as a URI object
  #   @option options [Float]    connect_timeout    Request-specific connect timeout (in seconds)
  #   @option options [Float]    socket_timeout     Request-specific socket timeout (in seconds)
  #   @option options [Float]    request_timeout    Request-specific request timeout (in seconds)
  #   @option options [Integer]  max_redirects      Request-specific maximum redirect limit
  #   @option options [Boolean]  follow_redirects   Specify whether this request should follow redirects
  #   @option options [Hash]     auth               Specify authentication for the request
  #   @option options [String]   auth[:user]        Username to auth with
  #   @option options [String]   auth[:password]    Password to auth with
  #   @option options [Boolean]  auth[:eager]       Eagerly offer the Authorization header before the server challenges for it.
  #                                                     You should not use this unless you know you specifically need it, as misuse
  #                                                     of it can leak user credentials.
  #
  # @!macro [new] http_request_exceptions
  #   @raise [Manticore::Timeout] on socket, connection, or response timeout
  #   @raise [Manticore::SocketException] on internal socket exception (ie, unexpected socket closure)
  #   @raise [Manticore::ClientProtocolException] on protocol errors such as an SSL handshake failure or connection exception
  #   @raise [Manticore::ResolutionFailure] on DNS resolution failure
  #   @return [Response]
  #
  # @!macro [new] http_method_shared_async
  #   @example Simple usage
  #     client.$0("http://example.com/some/resource", params: {foo: "bar"}, headers: {"X-Custom-Header" => "whee"}).
  #       on_success {|response|
  #         # Do something with response.body, response.code, etc
  #       }.on_failure {|exception|
  #         # Handle request exception
  #       }
  #     client.execute!
  #
  # @!macro [new] http_method_shared_sync
  #   @example Simple usage
  #     body = client.$0("http://example.com/some/resource", params: {foo: "bar"}, headers: {"X-Custom-Header" => "whee"}).body
  #   @example Passing a block as the success handler:
  #     body = client.$0("http://example.com/some/resource", params: {foo: "bar"}, headers: {"X-Custom-Header" => "whee"}) {|response| response.body }
  #   @example Explicit success handler:
  #     body = client.$0("http://example.com/some/resource", params: {foo: "bar"}, headers: {"X-Custom-Header" => "whee"}).
  #       on_success {|response| response.body }
  #   @macro http_method_shared
  #   @macro http_request_exceptions
  #
  # @!macro [new] http_method_shared_async_with_body
  #   @macro http_method_shared_async
  #   @option options [String] body     Body to pass with the request
  #
  # @!macro [new] http_method_shared_sync_with_body
  #   @macro http_method_shared_sync
  #   @option options [String] body     Body to pass with the request

  # Core Manticore client, with a backing {http://hc.apache.org/httpcomponents-client-ga/httpclient/apidocs/org/apache/http/impl/conn/PoolingHttpClientConnectionManager.html PoolingHttpClientConnectionManager}
  class Client
    include_package "org.apache.http.client.methods"
    include_package "org.apache.http.client.entity"
    include_package "org.apache.http.client.config"
    include_package "org.apache.http.config"
    include_package "org.apache.http.conn.socket"
    include_package "org.apache.http.impl"
    include_package "org.apache.http.impl.client"
    include_package "org.apache.http.impl.conn"
    include_package "org.apache.http.impl.auth"
    include_package "org.apache.http.entity"
    include_package "org.apache.http.message"
    include_package "org.apache.http.params"
    include_package "org.apache.http.protocol"
    include_package "org.apache.http.auth"
    include_package "java.util.concurrent"
    include_package "org.apache.http.client.protocol"
    include_package "org.apache.http.conn.ssl"
    include_package "java.security.cert"
    include_package "java.security.spec"
    include_package "java.security"
    include_package "org.apache.http.client.utils"
    java_import "org.apache.http.HttpHost"
    java_import "javax.net.ssl.SSLContext"
    java_import "org.manticore.HttpGetWithEntity"
    java_import "org.manticore.HttpDeleteWithEntity"
    java_import "org.apache.http.auth.UsernamePasswordCredentials"

    # This is a class rather than a proc because the proc holds a closure around
    # the instance of the Client that creates it.
    class ExecutorThreadFactory
      include ::Java::JavaUtilConcurrent::ThreadFactory

      def newThread(runnable)
        thread = Executors.defaultThreadFactory.newThread(runnable)
        thread.daemon = true
        return thread
      end
    end

    include ProxiesInterface

    # The default maximum pool size for requests
    DEFAULT_MAX_POOL_SIZE = 50

    DEFAULT_REQUEST_TIMEOUT = 60
    DEFAULT_SOCKET_TIMEOUT = 10
    DEFAULT_CONNECT_TIMEOUT = 10
    DEFAULT_MAX_REDIRECTS = 5
    DEFAULT_EXPECT_CONTINUE = false
    DEFAULT_STALE_CHECK = false

    attr_reader :client

    # Create a new HTTP client with a backing request pool. if you pass a block to the initializer, the underlying
    # {http://hc.apache.org/httpcomponents-client-ga/httpclient/apidocs/org/apache/http/impl/client/HttpClientBuilder.html HttpClientBuilder}
    # and {http://hc.apache.org/httpcomponents-client-ga/httpclient/apidocs/org/apache/http/client/config/RequestConfig.Builder.html RequestConfig.Builder}
    # will be yielded so that you can operate on them directly.
    #
    # @see http://hc.apache.org/httpcomponents-client-ga/httpclient/apidocs/org/apache/http/impl/client/HttpClientBuilder.html HttpClientBuilder
    # @see http://hc.apache.org/httpcomponents-client-ga/httpclient/apidocs/org/apache/http/client/config/RequestConfig.Builder.html RequestConfig.Builder
    # @example  Simple instantiation and usage
    #   client = Manticore::Client.new
    #   client.get("http://www.google.com")
    #
    # @example Instantiation with a block
    #   client = Manticore::Client.new(socket_timeout: 5) do |http_client_builder, request_builder|
    #     http_client_builder.disable_redirect_handling
    #   end
    #
    # @param options  [Hash]                                                   Client pool options
    # @option options [String]          user_agent                             The user agent used in requests.
    # @option options [Integer]         pool_max                   (50)        The maximum number of active connections in the pool
    # @option options [integer]         pool_max_per_route         (pool_max)  Sets the maximum number of active connections for a given target endpoint
    # @option options [boolean]         cookies                    (true)      enable or disable automatic cookie management between requests
    # @option options [boolean]         compression                (true)      enable or disable transparent gzip/deflate support
    # @option options [integer]         request_timeout            (60)        Sets the timeout for requests. Raises {Manticore::Timeout} on failure.
    # @option options [integer]         connect_timeout            (10)        Sets the timeout for connections. Raises Manticore::Timeout on failure.
    # @option options [integer]         socket_timeout             (10)        Sets SO_TIMEOUT for open connections. A value of 0 is an infinite timeout. Raises Manticore::Timeout on failure.
    # @option options [boolean]         tcp_no_delay               (true)      Enable or disable Nagle's algorithm
    # @option options [integer]         request_timeout            (60)        Sets the timeout for a given request. Raises Manticore::Timeout on failure.
    # @option options [integer]         max_redirects              (5)         Sets the maximum number of redirects to follow.
    # @option options [integer]         automatic_retries          (3)         Sets the number of times the client will automatically retry failed requests.
    # @option options [boolean]         retry_non_idempotent       (false)     If true, Manticore will automatically retry failed requests with non-idempotent verbs. Otherwise, it only automatically retries
    #                                                                            on GET, HEAD, PUT, DELETE, OPTIONS, and TRACE
    # @option options [boolean]         expect_continue            (false)     Enable support for HTTP 100
    # @option options [boolean]         stale_check                (false)     Enable support for stale connection checking. Adds overhead.
    # @option options [integer]         check_connection_timeout   (2_000)    Connections that haven't been used in this many milliseconds will be validated before being used. Set to a negative number to disable.
    # @option options [String]          proxy                                    Proxy host in form: http://proxy.org:1234
    # @option options [Hash]            proxy                                    Proxy host in form: {host: 'proxy.org'[, port: 80[, scheme: 'http'[, user: 'username@host', password: 'password']]]}
    # @option options [Hash]            proxy                                    Proxy host in form: {url:  'http://proxy.org:1234'[, user: 'username@host', password: 'password']]]}
    # @option options [URI]             proxy                                    Proxy host as a URI object
    # @option options [Boolean]         keepalive                  (true)      Whether to allow connections to be reused. Defaults to true.
    # @option options [Hash]            ssl                                        Hash of options for configuring SSL
    # @option options [Array<String>]   ssl[:protocols]            (nil)       A list of protocols that Manticore should accept
    # @option options [Array<String>]   ssl[:cipher_suites]        (nil)       A list of cipher suites that Manticore should accept
    # @option options [Symbol]          ssl[:verify]               (:strict)   Hostname verification setting. Set to `:disable` to turn off hostname verification. Setting to `:browser` will
    #                                                                            cause Manticore to accept a certificate for *.foo.com for all subdomains and sub-subdomains (eg a.b.foo.com).
    #                                                                            The default `:strict` is like `:browser` except it'll only accept a single level of subdomains for wildcards,
    #                                                                            eg `b.foo.com` will be accepted for a `*.foo.com` certificate, but `a.b.foo.com` will not be.
    # @option options [String]          ssl[:truststore]          (nil)        Path to a custom trust store to use the verifying SSL connections
    # @option options [String]          ssl[:truststore_password] (nil)        Password used for decrypting the server trust store
    # @option options [String]          ssl[:truststore_type]     (nil)        Format of the trust store, ie "JKS" or "PKCS12". If left nil, the type will be inferred from the truststore filename.
    # @option options [String]          ssl[:keystore]            (nil)        Path to a custom key store to use for client certificate authentication
    # @option options [String]          ssl[:keystore_password]   (nil)        Password used for decrypting the client auth key store
    # @option options [String]          ssl[:keystore_type]       (nil)        Format of the key store, ie "JKS" or "PKCS12". If left nil, the type will be inferred from the keystore filename.
    # @option options [String]          ssl[:ca_file]             (nil)        OpenSSL-style path to an X.509 certificate to use to validate SSL certificates
    # @option options [String|OpenSSL::X509::Certificate]   ssl[:client_cert]         (nil)        A string containing a base64-encoded X.509 certificate, OR a path to an OpenSSL-style X.509 certificate, OR an instance of OpenSSL::X509::Certificate
    # @option options [String|OpenSSL::PKey::Pkey]          ssl[:client_key]          (nil)        A string containing a base64-encoded RSA key to use for client authentication, OR a path to an OpenSSL-style RSA key, OR an instance of OpenSSL::PKey::PKey
    # @option options [boolean]         ssl[:track_state]         (false)      Turn on or off connection state tracking. This helps prevent SSL information from leaking across threads, but means that connections
    #                                                                             can't be shared across those threads. This should generally be left off unless you know what you're doing.
    def initialize(options = {})
      @finalizers = []
      self.class.shutdown_on_finalize self, @finalizers

      builder = client_builder
      builder.set_user_agent options.fetch(:user_agent, "Manticore #{VERSION}")
      @options = options
      @use_cookies = options.fetch(:cookies, false)
      builder.disable_cookie_management unless @use_cookies
      builder.disable_content_compression if options.fetch(:compression, true) == false
      builder.set_proxy get_proxy_host(options[:proxy]) if options.key?(:proxy)

      builder.set_retry_handler LoggingStandardRetryHandler.new options.fetch(:automatic_retries, 3), options.fetch(:retry_non_idempotent, false)

      # http://hc.apache.org/httpcomponents-client-ga/tutorial/html/advanced.html#stateful_conn
      # By default this is used to prevent different contexts from accessing SSL data
      # Since we're running this for JRuby which does not have context separation within the JVM
      # We can disable this for connection reuse.
      builder.disable_connection_state unless options.fetch(:ssl, {}).fetch(:track_state, false)

      @keepalive = options.fetch(:keepalive, true)
      if @keepalive == false
        builder.set_connection_reuse_strategy { |response, context| false }
      else
        builder.set_connection_reuse_strategy DefaultConnectionReuseStrategy.new
      end

      socket_config_builder = SocketConfig.custom
      socket_config_builder.set_so_timeout(options.fetch(:socket_timeout, DEFAULT_SOCKET_TIMEOUT) * 1000)
      socket_config_builder.set_tcp_no_delay(options.fetch(:tcp_no_delay, true))
      builder.set_default_socket_config socket_config_builder.build

      builder.set_connection_manager pool(options)

      request_config = RequestConfig.custom
      request_config.set_connection_request_timeout options.fetch(:request_timeout, DEFAULT_REQUEST_TIMEOUT) * 1000
      request_config.set_connect_timeout options.fetch(:connect_timeout, DEFAULT_CONNECT_TIMEOUT) * 1000
      request_config.set_socket_timeout options.fetch(:socket_timeout, DEFAULT_SOCKET_TIMEOUT) * 1000
      request_config.set_max_redirects options.fetch(:max_redirects, DEFAULT_MAX_REDIRECTS)
      request_config.set_expect_continue_enabled options.fetch(:expect_continue, DEFAULT_EXPECT_CONTINUE)
      request_config.set_stale_connection_check_enabled options.fetch(:stale_check, DEFAULT_STALE_CHECK)
      request_config.set_circular_redirects_allowed false

      yield builder, request_config if block_given?

      builder.set_default_request_config request_config.build
      @client = builder.build
      finalize @client, :close
      @options = options
      @async_requests = []
      @stubs = {}
    end

    # Return a hash of statistics about this client's HTTP connection pool
    def pool_stats
      stats = @pool.get_total_stats
      {
        max: stats.get_max,
        leased: stats.get_leased,
        pending: stats.get_pending,
        available: stats.get_available,
      }
    end

    ### Sync methods

    # Perform a HTTP GET request
    # @macro http_method_shared_sync
    def get(url, options = {}, &block)
      options = treat_params_as_query(options)
      request HttpGetWithEntity, url, options, &block
    end

    # Perform a HTTP PUT request
    # @macro http_method_shared_sync_with_body
    def put(url, options = {}, &block)
      request HttpPut, url, options, &block
    end

    # Perform a HTTP HEAD request
    # @macro http_method_shared_sync
    def head(url, options = {}, &block)
      options = treat_params_as_query(options)
      request HttpHead, url, options, &block
    end

    # Perform a HTTP POST request
    # @macro http_method_shared_sync_with_body
    def post(url, options = {}, &block)
      request HttpPost, url, options, &block
    end

    # Perform a HTTP DELETE request
    # @macro http_method_shared_sync
    def delete(url, options = {}, &block)
      options = treat_params_as_query(options)
      request HttpDeleteWithEntity, url, options, &block
    end

    # Perform a HTTP OPTIONS request
    # @macro http_method_shared_sync
    def options(url, options = {}, &block)
      request HttpOptions, url, options, &block
    end

    # Perform a HTTP PATCH request
    # @macro http_method_shared_sync_with_body
    def patch(url, options = {}, &block)
      request HttpPatch, url, options, &block
    end

    %w(get put head post delete options patch).each do |func|
      define_method "#{func}!" do |url, options, &block|
        send(func, url, options, &block).call
      end
    end

    # Perform an HTTP request, passing the method as a parameter
    # @param method [String, Symbol] Method to call (get put head post options patch)
    # @macro http_method_shared
    # @macro http_request_exceptions
    def http(method, url, options = {}, &block)
      case method.to_s.downcase
      when *%w(get put head post delete options patch)
        send method, url, options, &block
      else
        raise "Invalid method: #{method}"
      end
    end

    # Cause this client to return a stubbed response for this URL
    # @param  url [String] URL to stub for
    # @param  stubs [Hash] Hash of options to return for the stubbed response
    def stub(url, stubs)
      @stubs[url_as_regex(url)] = stubs
    end

    # Cause this client to unstubbed previously-stubbed URL
    def unstub(url)
      @stubs.delete(url_as_regex(url))
    end

    # Wipe all currently-set stubs.
    def clear_stubs!
      @stubs.clear
    end

    # Remove all pending asynchronous requests.
    #
    # @return nil
    def clear_pending
      @async_requests.clear
      nil
    end

    # Execute all queued async requests
    #
    # @return [Array] An array of the responses from the requests executed.
    def execute!
      method = executor.java_method(:submit, [java.util.concurrent.Callable.java_class])
      result = @async_requests.map { |r| method.call r }
      @async_requests.clear
      result.map do |future|
        begin
          future.get
        rescue Java::JavaUtilConcurrent::ExecutionException => e
          # These exceptions should be handled in on_failure blocks.
        end
      end
    end

    # Free resources associated with the CloseableHttpClient
    def close
      @client.close if @client
    end

    # Get at the underlying ExecutorService used to invoke asynchronous calls.
    def executor
      create_executor_if_needed
      @executor
    end

    def self.shutdown_on_finalize(client, objs)
      ObjectSpace.define_finalizer client, -> {
                                     objs.each { |obj, args| obj.send(*args) rescue nil }
                                   }
    end

    protected

    # Takes an object and a message to pass to the object to destroy it. This is done rather than
    # a proc to avoid creating a closure that would maintain a reference to this client, which
    # would prevent the client from being cleaned up.
    def finalize(object, args)
      @finalizers << [WeakRef.new(object), Array(args)]
    end

    def url_as_regex(url)
      if url.is_a?(String)
        %r{^#{Regexp.escape url}$}
      else
        url
      end
    end

    def client_builder
      HttpClientBuilder.create
    end

    def pool_builder(options)
      http_sf = PlainConnectionSocketFactory.new

      # :nocov:
      if options[:ignore_ssl_validation]
        $stderr.puts "The options[:ignore_ssl_validation] setting is deprecated in favor of options[:ssl][:verify]"
        options[:ssl] ||= {}
        options[:ssl] = {:verify => !options.delete(:ignore_ssl_validation)}.merge(options[:ssl])
      end
      # :nocov:

      https_sf = ssl_socket_factory_from_options options.fetch(:ssl, {})
      registry = RegistryBuilder.create.register("http", http_sf).register("https", https_sf).build
      PoolingHttpClientConnectionManager.new(registry)
    end

    def pool(options = {})
      @pool ||= begin
        @max_pool_size = options.fetch(:pool_max, DEFAULT_MAX_POOL_SIZE)
        pool_builder(options).tap do |cm|
          cm.set_validate_after_inactivity options.fetch(:check_connection_timeout, 2_000)
          cm.set_default_max_per_route options.fetch(:pool_max_per_route, @max_pool_size)
          cm.set_max_total @max_pool_size
          finalize cm, :shutdown
        end
      end
    end

    def create_executor_if_needed
      return @executor if @executor
      @executor = Executors.new_cached_thread_pool(ExecutorThreadFactory.new)
      finalize @executor, :shutdown
    end

    def request(klass, url, options, &block)
      req, context = request_from_options(klass, url, options)
      async = options.delete(:async)
      background = options.delete(:async_background)
      create_executor_if_needed if (background || async)
      response = response_object_for(req, context, &block)

      if async
        @async_requests << response
      elsif background
        response.background = true
      end

      if block_given? && (background || !async)
        response.call
      else
        response
      end
    end

    def response_object_for(request, context, &block)
      request_uri = request.getURI.to_s

      match_key = @stubs.keys.find { |k| request_uri.match(k) }
      if match_key
        StubbedResponse.new(self, request, context, &block).stub(@stubs[match_key])
      else
        Response.new(self, request, context, &block)
      end
    end

    def uri_from_url_and_options(url, options)
      url = url.to_s if url.is_a?(URI)
      builder = URIBuilder.new(url)
      pairs = struct_to_name_value_pairs(options[:query])
      builder.add_parameters pairs unless pairs.empty?
      builder.to_string
    end

    def request_from_options(klass, url, options)
      req = klass.new uri_from_url_and_options(url, options).to_s

      if (options[:params] || options[:body] || options[:entity]) && req.kind_of?(HttpEntityEnclosingRequestBase)
        if options[:params]
          pairs = struct_to_name_value_pairs(options[:params])
          encoding = minimum_encoding_for options[:params].to_s
          req.set_entity UrlEncodedFormEntity.new(pairs, encoding)
        elsif options[:body]
          if options[:body].encoding == Encoding::ASCII_8BIT
            req.set_entity ByteArrayEntity.new(options[:body].to_java_bytes)
          else
            req.set_entity StringEntity.new(options[:body], minimum_encoding_for(options[:body]))
          end
        elsif options[:entity]
          req.set_entity options[:entity]
        end
      end

      req_options = @options.merge(options)
      if options.key?(:proxy) || options.key?(:connect_timeout) || options.key?(:socket_timeout) || options.key?(:max_redirects) || options.key?(:follow_redirects)
        config = RequestConfig.custom()

        if req_options[:proxy]
          config.set_proxy get_proxy_host(req_options[:proxy])
        end
        config.set_max_redirects req_options[:max_redirects] if req_options[:max_redirects]
        config.set_redirects_enabled !!req_options[:follow_redirects] if req_options.fetch(:follow_redirects, nil) != nil
        config.set_connect_timeout req_options[:connect_timeout] * 1000 if req_options[:connect_timeout]
        config.set_socket_timeout req_options[:socket_timeout] * 1000 if req_options[:socket_timeout]
        config.set_connection_request_timeout req_options[:request_timeout] * 1000 if req_options[:request_timeout]
        req.set_config config.build
      end

      headers = []
      # Support keepalive on HTTP/1.0 connections
      headers.push BasicHeader.new("Connection", "Keep-Alive") if @keepalive

      if options.key?(:headers)
        options[:headers].each do |k, v|
          Array(v).each do |_v|
            headers.push BasicHeader.new(k, _v)
          end
        end
      end
      req.set_headers headers.to_java(BasicHeader) unless headers.empty?

      context = HttpClientContext.new
      proxy_user = req_options[:proxy].is_a?(Hash) && (req_options[:proxy][:user] || req_options[:proxy][:username])
      auth_from_options(req, req_options, context)

      if @use_cookies == :per_request
        store = BasicCookieStore.new
        context.setAttribute(ClientContext.COOKIE_STORE, store)
      end

      return req, context
    end

    def get_proxy_host(opt)
      host = nil
      if opt.is_a? String
        uri = URI.parse(opt)
        if uri.host
          get_proxy_host uri
        else
          uri = URI.parse("http://#{opt}")
          get_proxy_host uri
        end
      elsif opt.is_a? Hash
        if opt.key?(:url)
          get_proxy_host URI.parse(opt[:url])
        elsif opt.key?(:host)
          HttpHost.new(opt[:host], (opt[:port] || 80).to_i, opt[:scheme] || "http")
        end
      elsif opt.is_a? URI
        opt.scheme ||= "http"
        opt.port ||= 80
        HttpHost.new(opt.host, opt.port, opt.scheme)
      end
    end

    def auth_from_options(req, options, context)
      proxy = options.fetch(:proxy, {})

      proxy_user, proxy_pass = if proxy.is_a?(String)
                                 proxy_uri = URI.parse(proxy)
                                 [proxy_uri.user, proxy_uri.password]
                               else
                                 [(proxy[:user] || proxy[:username]),
                                  (proxy[:pass] || proxy[:password])]
                               end

      if options[:auth] || proxy_user
        provider = BasicCredentialsProvider.new
        if options[:auth]
          username = options[:auth][:user] || options[:auth][:username]
          password = options[:auth][:pass] || options[:auth][:password]
          provider.set_credentials AuthScope::ANY, UsernamePasswordCredentials.new(username, password)

          if options[:auth][:eager]
            uri = URI.parse req.uri.to_string
            target = HttpHost.new(uri.host, uri.port, uri.scheme)
            scheme = BasicScheme.new

            cache = BasicAuthCache.new
            cache.put target, scheme
            context.set_auth_cache cache
          end
        end

        if proxy_user
          provider.set_credentials AuthScope.new(get_proxy_host(proxy)), UsernamePasswordCredentials.new(proxy_user, proxy_pass)
        end
        context.set_credentials_provider(provider)
      end
    end

    def struct_to_name_value_pairs(value, namespace = nil)
      case value
      when nil
        []
      when Hash
        value.flat_map { |key, val| struct_to_name_value_pairs val, namespace ? "#{namespace}[#{key}]" : key }
      when Array
        value.flat_map { |val| struct_to_name_value_pairs val, namespace }
      else
        BasicNameValuePair.new(namespace, value.to_s)
      end
    end

    # Apache HTTP assumes ISO_8859_1 for StringEntities; we'll try to be nice and pass that when possible
    # so that it doesn't have to any multibyte work.
    ISO_8859_1 = "ISO-8859-1".freeze

    def minimum_encoding_for(string)
      if string.ascii_only?
        ISO_8859_1
      else
        string.encoding.to_s
      end
    end

    # Configure the SSL Context
    def ssl_socket_factory_from_options(ssl_options)
      trust_store = trust_strategy = nil

      verifier = SSLConnectionSocketFactory::STRICT_HOSTNAME_VERIFIER
      case ssl_options.fetch(:verify, :strict)
      when false, :disable, :none
        trust_store = nil
        trust_strategy = TrustSelfSignedStrategy.new
        verifier = SSLConnectionSocketFactory::ALLOW_ALL_HOSTNAME_VERIFIER
      when :browser
        verifier = SSLConnectionSocketFactory::BROWSER_COMPATIBLE_HOSTNAME_VERIFIER
      when true, :strict, :default
        verifier = SSLConnectionSocketFactory::STRICT_HOSTNAME_VERIFIER
      else
        raise "Invalid value for :verify. Valid values are (:all, :browser, :default)"
      end

      context = SSLContexts.custom
      setup_trust_store ssl_options, context, trust_strategy
      setup_key_store ssl_options, context

      SSLConnectionSocketFactory.new context.build, ssl_options[:protocols].to_java(:string), ssl_options[:cipher_suites].to_java(:string), verifier
    end

    def setup_trust_store(ssl_options, context, trust_strategy)
      trust_store = get_store(:truststore, ssl_options) if ssl_options.key?(:truststore)

      # Support OpenSSL-style ca_file. We don't support ca_path for now.
      if ssl_options[:ca_file]
        trust_store ||= blank_keystore
        open(ssl_options[:ca_file]) do |fp|
          cert_collection = CertificateFactory.get_instance("X509").generate_certificates(fp.to_inputstream).to_a
          cert_collection.each do |cert|
            trust_store.set_certificate_entry(cert.getSubjectX500Principal.name, cert)
          end
        end
      end

      context.load_trust_material(trust_store, trust_strategy)
    end

    KEY_EXTRACTION_REGEXP = /(?:^-----BEGIN(.* )PRIVATE KEY-----\n)(.*?)(?:-----END\1PRIVATE KEY.*$)/m

    def setup_key_store(ssl_options, context)
      key_store = get_store(:keystore, ssl_options) if ssl_options.key?(:keystore)
      keystore_password = (ssl_options[:keystore_password] || "").to_java.toCharArray

      # Support OpenSSL-style bare X.509 certs with an RSA key
      if ssl_options[:client_cert] && ssl_options[:client_key]
        key_store ||= blank_keystore
        certs, key = nil, nil

        cert_str = if ssl_options[:client_cert].is_a?(OpenSSL::X509::Certificate)
                     ssl_options[:client_cert].to_s
                   elsif ssl_options[:client_cert].is_a?(String) && File.exists?(ssl_options[:client_cert])
                     File.read(ssl_options[:client_cert])
                   else
                     ssl_options[:client_cert].to_s
                   end

        cert_stream = java.io.ByteArrayInputStream.new(cert_str.strip.to_java_bytes)
        certs = CertificateFactory.get_instance("X509").generate_certificates(cert_stream).to_array([].to_java(Certificate))

        key_str = if ssl_options[:client_key].is_a?(OpenSSL::PKey::PKey)
                    ssl_options[:client_key].to_pem_pkcs8
                  elsif ssl_options[:client_key].is_a?(String) && File.exists?(ssl_options[:client_key])
                    File.read(ssl_options[:client_key])
                  else
                    ssl_options[:client_key].to_s
                  end

        # Add each of the keys in the given keyfile into the keystore.
        key_parts = key_str.scan(KEY_EXTRACTION_REGEXP)
        key_parts.each do |type, b64key|
          body = Base64.decode64 b64key
          spec = PKCS8EncodedKeySpec.new(body.strip.to_java_bytes)
          type = type.strip
          type = "RSA" if type == ""
          key = KeyFactory.getInstance(type).generatePrivate(spec)
          key_store.set_key_entry("key-#{Digest::SHA1.hexdigest(body)}", key, keystore_password, certs)
        end
      end

      context.load_key_material(key_store, keystore_password) if key_store
    end

    def get_store(prefix, options)
      KeyStore.get_instance(options[:"#{prefix}_type"] || guess_store_type(options[prefix])).tap do |store|
        instream = open(options[prefix], "rb").to_inputstream
        store.load(instream, options.fetch(:"#{prefix}_password", nil).to_java.toCharArray)
      end
    end

    def blank_keystore
      KeyStore.get_instance(KeyStore.get_default_type).tap { |k| k.load(nil, nil) }
    end

    def guess_store_type(filename)
      if filename.end_with?(".p12") || filename.end_with?(".pfx")
        "pkcs12"
      else
        KeyStore.get_default_type
      end
    end

    def treat_params_as_query(options)
      if options.key?(:params) && !options.key?(:query)
        options.dup.tap { |o| o[:query] = o.delete(:params) }
      else
        options
      end
    end
  end

  class LoggingStandardRetryHandler < Java::OrgApacheHttpImplClient::StandardHttpRequestRetryHandler
    def retryRequest(exception, executionCount, context)
      context.setAttribute "retryCount", executionCount
      super(exception, executionCount, context)
    end
  end
end
