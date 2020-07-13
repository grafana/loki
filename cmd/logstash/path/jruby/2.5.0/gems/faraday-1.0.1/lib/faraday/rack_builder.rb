# frozen_string_literal: true

require 'faraday/adapter_registry'

module Faraday
  # A Builder that processes requests into responses by passing through an inner
  # middleware stack (heavily inspired by Rack).
  #
  # @example
  #   Faraday::Connection.new(url: 'http://sushi.com') do |builder|
  #     builder.request  :url_encoded  # Faraday::Request::UrlEncoded
  #     builder.adapter  :net_http     # Faraday::Adapter::NetHttp
  #   end
  class RackBuilder
    # Used to detect missing arguments
    NO_ARGUMENT = Object.new

    attr_accessor :handlers

    # Error raised when trying to modify the stack after calling `lock!`
    class StackLocked < RuntimeError; end

    # borrowed from ActiveSupport::Dependencies::Reference &
    # ActionDispatch::MiddlewareStack::Middleware
    class Handler
      REGISTRY = Faraday::AdapterRegistry.new

      attr_reader :name

      def initialize(klass, *args, &block)
        @name = klass.to_s
        REGISTRY.set(klass) if klass.respond_to?(:name)
        @args = args
        @block = block
      end

      def klass
        REGISTRY.get(@name)
      end

      def inspect
        @name
      end

      def ==(other)
        if other.is_a? Handler
          name == other.name
        elsif other.respond_to? :name
          klass == other
        else
          @name == other.to_s
        end
      end

      def build(app = nil)
        klass.new(app, *@args, &@block)
      end
    end

    def initialize(handlers = [], adapter = nil, &block)
      @adapter = adapter
      @handlers = handlers
      if block_given?
        build(&block)
      elsif @handlers.empty?
        # default stack, if nothing else is configured
        request :url_encoded
        self.adapter Faraday.default_adapter
      end
    end

    def build(options = {})
      raise_if_locked
      @handlers.clear unless options[:keep]
      yield(self) if block_given?
      adapter(Faraday.default_adapter) unless @adapter
    end

    def [](idx)
      @handlers[idx]
    end

    # Locks the middleware stack to ensure no further modifications are made.
    def lock!
      @handlers.freeze
    end

    def locked?
      @handlers.frozen?
    end

    def use(klass, *args, &block)
      if klass.is_a? Symbol
        use_symbol(Faraday::Middleware, klass, *args, &block)
      else
        raise_if_locked
        raise_if_adapter(klass)
        @handlers << self.class::Handler.new(klass, *args, &block)
      end
    end

    def request(key, *args, &block)
      use_symbol(Faraday::Request, key, *args, &block)
    end

    def response(key, *args, &block)
      use_symbol(Faraday::Response, key, *args, &block)
    end

    def adapter(klass = NO_ARGUMENT, *args, &block)
      return @adapter if klass == NO_ARGUMENT

      klass = Faraday::Adapter.lookup_middleware(klass) if klass.is_a?(Symbol)
      @adapter = self.class::Handler.new(klass, *args, &block)
    end

    ## methods to push onto the various positions in the stack:

    def insert(index, *args, &block)
      raise_if_locked
      index = assert_index(index)
      handler = self.class::Handler.new(*args, &block)
      @handlers.insert(index, handler)
    end

    alias insert_before insert

    def insert_after(index, *args, &block)
      index = assert_index(index)
      insert(index + 1, *args, &block)
    end

    def swap(index, *args, &block)
      raise_if_locked
      index = assert_index(index)
      @handlers.delete_at(index)
      insert(index, *args, &block)
    end

    def delete(handler)
      raise_if_locked
      @handlers.delete(handler)
    end

    # Processes a Request into a Response by passing it through this Builder's
    # middleware stack.
    #
    # @param connection [Faraday::Connection]
    # @param request [Faraday::Request]
    #
    # @return [Faraday::Response]
    def build_response(connection, request)
      app.call(build_env(connection, request))
    end

    # The "rack app" wrapped in middleware. All requests are sent here.
    #
    # The builder is responsible for creating the app object. After this,
    # the builder gets locked to ensure no further modifications are made
    # to the middleware stack.
    #
    # Returns an object that responds to `call` and returns a Response.
    def app
      @app ||= begin
        lock!
        to_app
      end
    end

    def to_app
      # last added handler is the deepest and thus closest to the inner app
      # adapter is always the last one
      @handlers.reverse.inject(@adapter.build) do |app, handler|
        handler.build(app)
      end
    end

    def ==(other)
      other.is_a?(self.class) &&
        @handlers == other.handlers &&
        @adapter == other.adapter
    end

    def dup
      self.class.new(@handlers.dup, @adapter.dup)
    end

    # ENV Keys
    # :method - a symbolized request method (:get, :post)
    # :body   - the request body that will eventually be converted to a string.
    # :url    - URI instance for the current request.
    # :status           - HTTP response status code
    # :request_headers  - hash of HTTP Headers to be sent to the server
    # :response_headers - Hash of HTTP headers from the server
    # :parallel_manager - sent if the connection is in parallel mode
    # :request - Hash of options for configuring the request.
    #   :timeout      - open/read timeout Integer in seconds
    #   :open_timeout - read timeout Integer in seconds
    #   :proxy        - Hash of proxy options
    #     :uri        - Proxy Server URI
    #     :user       - Proxy server username
    #     :password   - Proxy server password
    # :ssl - Hash of options for configuring SSL requests.
    def build_env(connection, request)
      exclusive_url = connection.build_exclusive_url(
        request.path, request.params,
        request.options.params_encoder
      )

      Env.new(request.method, request.body, exclusive_url,
              request.options, request.headers, connection.ssl,
              connection.parallel_manager)
    end

    private

    LOCK_ERR = "can't modify middleware stack after making a request"

    def raise_if_locked
      raise StackLocked, LOCK_ERR if locked?
    end

    def raise_if_adapter(klass)
      return unless is_adapter?(klass)

      raise 'Adapter should be set using the `adapter` method, not `use`'
    end

    def adapter_set?
      !@adapter.nil?
    end

    def is_adapter?(klass) # rubocop:disable Naming/PredicateName
      klass.ancestors.include?(Faraday::Adapter)
    end

    def use_symbol(mod, key, *args, &block)
      use(mod.lookup_middleware(key), *args, &block)
    end

    def assert_index(index)
      idx = index.is_a?(Integer) ? index : @handlers.index(index)
      raise "No such handler: #{index.inspect}" unless idx

      idx
    end
  end
end
