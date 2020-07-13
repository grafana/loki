# frozen_string_literal: true

require 'uri'

module Faraday
  class Adapter
    # EventMachine Synchrony adapter.
    class EMSynchrony < Faraday::Adapter
      include EMHttp::Options

      dependency do
        require 'em-synchrony/em-http'
        require 'em-synchrony/em-multi'
        require 'fiber'
      end

      self.supports_parallel = true

      # @return [ParallelManager]
      def self.setup_parallel_manager(_options = nil)
        ParallelManager.new
      end

      def call(env)
        super
        request = create_request(env)

        http_method = env[:method].to_s.downcase.to_sym

        if env[:parallel_manager]
          # Queue requests for parallel execution.
          execute_parallel_request(env, request, http_method)
        else
          # Execute single request.
          execute_single_request(env, request, http_method)
        end

        @app.call env
      rescue Errno::ECONNREFUSED
        raise Faraday::ConnectionFailed, $ERROR_INFO
      rescue EventMachine::Connectify::CONNECTError => e
        if e.message.include?('Proxy Authentication Required')
          raise Faraday::ConnectionFailed,
                %(407 "Proxy Authentication Required")
        end

        raise Faraday::ConnectionFailed, e
      rescue Errno::ETIMEDOUT => e
        raise Faraday::TimeoutError, e
      rescue RuntimeError => e
        if e.message == 'connection closed by server'
          raise Faraday::ConnectionFailed, e
        end

        raise Faraday::TimeoutError, e if e.message.include?('timeout error')

        raise
      rescue StandardError => e
        if defined?(OpenSSL) && e.is_a?(OpenSSL::SSL::SSLError)
          raise Faraday::SSLError, e
        end

        raise
      end

      def create_request(env)
        EventMachine::HttpRequest.new(
          Utils::URI(env[:url].to_s),
          connection_config(env).merge(@connection_options)
        )
      end

      private

      def execute_parallel_request(env, request, http_method)
        env[:parallel_manager].add(request, http_method,
                                   request_config(env)) do |resp|
          if (req = env[:request]).stream_response?
            warn "Streaming downloads for #{self.class.name} " \
                'are not yet implemented.'
            req.on_data.call(resp.response, resp.response.bytesize)
          end

          save_response(env, resp.response_header.status,
                        resp.response) do |resp_headers|
            resp.response_header.each do |name, value|
              resp_headers[name.to_sym] = value
            end
          end

          # Finalize the response object with values from `env`.
          env[:response].finish(env)
        end
      end

      def execute_single_request(env, request, http_method)
        block = -> { request.send(http_method, request_config(env)) }
        client = call_block(block)

        raise client.error if client&.error

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

      def call_block(block)
        client = nil

        if EM.reactor_running?
          client = block.call
        else
          EM.run do
            Fiber.new do
              client = block.call
              EM.stop
            end.resume
          end
        end

        client
      end
    end
  end
end

require 'faraday/adapter/em_synchrony/parallel_manager'

if Faraday::Adapter::EMSynchrony.loaded?
  begin
    require 'openssl'
  rescue LoadError
    warn 'Warning: no such file to load -- openssl. ' \
      'Make sure it is installed if you want HTTPS support'
  else
    require 'faraday/adapter/em_http_ssl_patch'
  end
end
