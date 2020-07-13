# frozen_string_literal: true

module Faraday
  class Adapter
    # EventMachine adapter. This adapter is useful for either asynchronous
    # requests when in an EM reactor loop, or for making parallel requests in
    # synchronous code.
    class EMHttp < Faraday::Adapter
      # Options is a module containing helpers to convert the Faraday env object
      # into options hashes for EMHTTP method calls.
      module Options
        # @return [Hash]
        def connection_config(env)
          options = {}
          configure_proxy(options, env)
          configure_timeout(options, env)
          configure_socket(options, env)
          configure_ssl(options, env)
          options
        end

        def request_config(env)
          options = {
            body: read_body(env),
            head: env[:request_headers]
            # keepalive: true,
            # file: 'path/to/file', # stream data off disk
          }
          configure_compression(options, env)
          options
        end

        def read_body(env)
          body = env[:body]
          body.respond_to?(:read) ? body.read : body
        end

        # Reads out proxy settings from env into options
        def configure_proxy(options, env)
          proxy = request_options(env)[:proxy]
          return unless proxy

          options[:proxy] = {
            host: proxy[:uri].host,
            port: proxy[:uri].port,
            authorization: [proxy[:user], proxy[:password]]
          }
        end

        # Reads out host and port settings from env into options
        def configure_socket(options, env)
          bind = request_options(env)[:bind]
          return unless bind

          options[:bind] = {
            host: bind[:host],
            port: bind[:port]
          }
        end

        # Reads out SSL certificate settings from env into options
        def configure_ssl(options, env)
          return unless env[:url].scheme == 'https' && env[:ssl]

          options[:ssl] = {
            cert_chain_file: env[:ssl][:ca_file],
            verify_peer: env[:ssl].fetch(:verify, true)
          }
        end

        # Reads out timeout settings from env into options
        def configure_timeout(options, env)
          req = request_options(env)
          options[:inactivity_timeout] = request_timeout(:read, req)
          options[:connect_timeout] = request_timeout(:open, req)
        end

        # Reads out compression header settings from env into options
        def configure_compression(options, env)
          return unless (env[:method] == :get) &&
                        !options[:head].key?('accept-encoding')

          options[:head]['accept-encoding'] = 'gzip, compressed'
        end

        def request_options(env)
          env[:request]
        end
      end

      include Options

      dependency 'em-http'

      self.supports_parallel = true

      # @return [Manager]
      def self.setup_parallel_manager(_options = nil)
        Manager.new
      end

      def call(env)
        super
        perform_request env
        @app.call env
      end

      def perform_request(env)
        if parallel?(env)
          manager = env[:parallel_manager]
          manager.add do
            perform_single_request(env)
              .callback { env[:response].finish(env) }
          end
        elsif EventMachine.reactor_running?
          # EM is running: instruct upstream that this is an async request
          env[:parallel_manager] = true
          perform_single_request(env)
            .callback { env[:response].finish(env) }
            .errback do
              # TODO: no way to communicate the error in async mode
              raise NotImplementedError
            end
        else
          error = nil
          # start EM, block until request is completed
          EventMachine.run do
            perform_single_request(env)
              .callback { EventMachine.stop }
              .errback do |client|
                error = error_message(client)
                EventMachine.stop
              end
          end
          raise_error(error) if error
        end
      rescue EventMachine::Connectify::CONNECTError => e
        if e.message.include?('Proxy Authentication Required')
          raise Faraday::ConnectionFailed,
                %(407 "Proxy Authentication Required ")
        end

        raise Faraday::ConnectionFailed, e
      rescue StandardError => e
        if defined?(::OpenSSL::SSL::SSLError) && \
           e.is_a?(::OpenSSL::SSL::SSLError)
          raise Faraday::SSLError, e
        end

        raise
      end

      # TODO: reuse the connection to support pipelining
      def perform_single_request(env)
        req = create_request(env)
        req = req.setup_request(env[:method], request_config(env))
        req.callback do |client|
          if env[:request].stream_response?
            warn "Streaming downloads for #{self.class.name} " \
              'are not yet implemented.'
            env[:request].on_data.call(
              client.response,
              client.response.bytesize
            )
          end
          status = client.response_header.status
          reason = client.response_header.http_reason
          save_response(env, status, client.response, nil, reason) do |headers|
            client.response_header.each do |name, value|
              headers[name.to_sym] = value
            end
          end
        end
      end

      def create_request(env)
        EventMachine::HttpRequest.new(
          env[:url], connection_config(env).merge(@connection_options)
        )
      end

      def error_message(client)
        client.error || 'request failed'
      end

      def raise_error(msg)
        error_class = Faraday::ClientError
        if timeout_message?(msg)
          error_class = Faraday::TimeoutError
          msg = 'request timed out'
        elsif msg == Errno::ECONNREFUSED
          error_class = Faraday::ConnectionFailed
          msg = 'connection refused'
        elsif msg == 'connection closed by server'
          error_class = Faraday::ConnectionFailed
        end
        raise error_class, msg
      end

      def timeout_message?(msg)
        msg == Errno::ETIMEDOUT ||
          (msg.is_a?(String) && msg.include?('timeout error'))
      end

      # @return [Boolean]
      def parallel?(env)
        !!env[:parallel_manager]
      end

      # This parallel manager is designed to start an EventMachine loop
      # and block until all registered requests have been completed.
      class Manager
        # @see reset
        def initialize
          reset
        end

        # Re-initializes instance variables
        def reset
          @registered_procs = []
          @num_registered = 0
          @num_succeeded = 0
          @errors = []
          @running = false
        end

        # @return [Boolean]
        def running?
          @running
        end

        def add(&block)
          if running?
            perform_request { yield }
          else
            @registered_procs << block
          end
          @num_registered += 1
        end

        def run
          if @num_registered.positive?
            @running = true
            EventMachine.run do
              @registered_procs.each do |proc|
                perform_request(&proc)
              end
            end
            unless @errors.empty?
              raise Faraday::ClientError, @errors.first || 'connection failed'
            end
          end
        ensure
          reset
        end

        def perform_request
          client = yield
          client.callback do
            @num_succeeded += 1
            check_finished
          end
          client.errback do
            @errors << client.error
            check_finished
          end
        end

        def check_finished
          EventMachine.stop if @num_succeeded + @errors.size == @num_registered
        end
      end
    end
  end
end

if Faraday::Adapter::EMHttp.loaded?
  begin
    require 'openssl'
  rescue LoadError
    warn 'Warning: no such file to load -- openssl. ' \
      'Make sure it is installed if you want HTTPS support'
  else
    require 'faraday/adapter/em_http_ssl_patch'
  end
end
