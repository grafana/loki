# frozen_string_literal: true

module Faraday
  # Base class for all Faraday adapters. Adapters are
  # responsible for fulfilling a Faraday request.
  class Adapter
    extend MiddlewareRegistry
    extend DependencyLoader

    CONTENT_LENGTH = 'Content-Length'

    register_middleware File.expand_path('adapter', __dir__),
                        test: [:Test, 'test'],
                        net_http: [:NetHttp, 'net_http'],
                        net_http_persistent: [
                          :NetHttpPersistent,
                          'net_http_persistent'
                        ],
                        typhoeus: [:Typhoeus, 'typhoeus'],
                        patron: [:Patron, 'patron'],
                        em_synchrony: [:EMSynchrony, 'em_synchrony'],
                        em_http: [:EMHttp, 'em_http'],
                        excon: [:Excon, 'excon'],
                        rack: [:Rack, 'rack'],
                        httpclient: [:HTTPClient, 'httpclient']

    # This module marks an Adapter as supporting parallel requests.
    module Parallelism
      attr_writer :supports_parallel
      def supports_parallel?
        @supports_parallel
      end

      def inherited(subclass)
        super
        subclass.supports_parallel = supports_parallel?
      end
    end

    extend Parallelism
    self.supports_parallel = false

    def initialize(_app = nil, opts = {}, &block)
      @app = ->(env) { env.response }
      @connection_options = opts
      @config_block = block
    end

    # Yields or returns an adapter's configured connection. Depends on
    # #build_connection being defined on this adapter.
    #
    # @param env [Faraday::Env, Hash] The env object for a faraday request.
    #
    # @return The return value of the given block, or the HTTP connection object
    #         if no block is given.
    def connection(env)
      conn = build_connection(env)
      return conn unless block_given?

      yield conn
    end

    # Close any persistent connections. The adapter should still be usable
    # after calling close.
    def close
      # Possible implementation:
      #   @app.close if @app.respond_to?(:close)
    end

    def call(env)
      env.clear_body if env.needs_body?
      env.response = Response.new
    end

    private

    def save_response(env, status, body, headers = nil, reason_phrase = nil)
      env.status = status
      env.body = body
      env.reason_phrase = reason_phrase&.to_s&.strip
      env.response_headers = Utils::Headers.new.tap do |response_headers|
        response_headers.update headers unless headers.nil?
        yield(response_headers) if block_given?
      end

      env.response.finish(env) unless env.parallel?
      env.response
    end

    # Fetches either a read, write, or open timeout setting. Defaults to the
    # :timeout value if a more specific one is not given.
    #
    # @param type [Symbol] Describes which timeout setting to get: :read,
    #                      :write, or :open.
    # @param options [Hash] Hash containing Symbol keys like :timeout,
    #                       :read_timeout, :write_timeout, :open_timeout, or
    #                       :timeout
    #
    # @return [Integer, nil] Timeout duration in seconds, or nil if no timeout
    #                        has been set.
    def request_timeout(type, options)
      key = TIMEOUT_KEYS.fetch(type) do
        msg = "Expected :read, :write, :open. Got #{type.inspect} :("
        raise ArgumentError, msg
      end
      options[key] || options[:timeout]
    end

    TIMEOUT_KEYS = {
      read: :read_timeout,
      open: :open_timeout,
      write: :write_timeout
    }.freeze
  end
end
