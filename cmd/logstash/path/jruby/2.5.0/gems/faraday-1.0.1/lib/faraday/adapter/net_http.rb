# frozen_string_literal: true

begin
  require 'net/https'
rescue LoadError
  warn 'Warning: no such file to load -- net/https. ' \
    'Make sure openssl is installed if you want ssl support'
  require 'net/http'
end
require 'zlib'

module Faraday
  class Adapter
    # Net::HTTP adapter.
    class NetHttp < Faraday::Adapter
      exceptions = [
        IOError,
        Errno::EADDRNOTAVAIL,
        Errno::ECONNABORTED,
        Errno::ECONNREFUSED,
        Errno::ECONNRESET,
        Errno::EHOSTUNREACH,
        Errno::EINVAL,
        Errno::ENETUNREACH,
        Errno::EPIPE,
        Net::HTTPBadResponse,
        Net::HTTPHeaderSyntaxError,
        Net::ProtocolError,
        SocketError,
        Zlib::GzipFile::Error
      ]

      if defined?(::OpenSSL::SSL::SSLError)
        exceptions << ::OpenSSL::SSL::SSLError
      end
      exceptions << ::Net::OpenTimeout if defined?(::Net::OpenTimeout)

      NET_HTTP_EXCEPTIONS = exceptions.freeze

      def initialize(app = nil, opts = {}, &block)
        @ssl_cert_store = nil
        super(app, opts, &block)
      end

      def build_connection(env)
        net_http_connection(env).tap do |http|
          if http.respond_to?(:use_ssl=)
            http.use_ssl = env[:url].scheme == 'https'
          end
          configure_ssl(http, env[:ssl])
          configure_request(http, env[:request])
        end
      end

      def net_http_connection(env)
        klass = if (proxy = env[:request][:proxy])
                  Net::HTTP::Proxy(proxy[:uri].hostname, proxy[:uri].port,
                                   proxy[:user], proxy[:password])
                else
                  Net::HTTP
                end
        port = env[:url].port || (env[:url].scheme == 'https' ? 443 : 80)
        klass.new(env[:url].hostname, port)
      end

      def call(env)
        super
        http_response = connection(env) do |http|
          begin
            perform_request(http, env)
          rescue *NET_HTTP_EXCEPTIONS => e
            if defined?(OpenSSL) && e.is_a?(OpenSSL::SSL::SSLError)
              raise Faraday::SSLError, e
            end

            raise Faraday::ConnectionFailed, e
          end
        end

        save_response(env, http_response.code.to_i,
                      http_response.body || +'', nil,
                      http_response.message) do |response_headers|
          http_response.each_header do |key, value|
            response_headers[key] = value
          end
        end

        @app.call env
      rescue Timeout::Error, Errno::ETIMEDOUT => e
        raise Faraday::TimeoutError, e
      end

      private

      def create_request(env)
        request = Net::HTTPGenericRequest.new \
          env[:method].to_s.upcase, # request method
          !!env[:body], # is there request body
          env[:method] != :head, # is there response body
          env[:url].request_uri, # request uri path
          env[:request_headers] # request headers

        if env[:body].respond_to?(:read)
          request.body_stream = env[:body]
        else
          request.body = env[:body]
        end
        request
      end

      def perform_request(http, env)
        if env[:request].stream_response?
          size = 0
          yielded = false
          http_response = request_with_wrapped_block(http, env) do |chunk|
            if chunk.bytesize.positive? || size.positive?
              yielded = true
              size += chunk.bytesize
              env[:request].on_data.call(chunk, size)
            end
          end
          env[:request].on_data.call(+'', 0) unless yielded
          # Net::HTTP returns something,
          # but it's not meaningful according to the docs.
          http_response.body = nil
          http_response
        else
          request_with_wrapped_block(http, env)
        end
      end

      def request_with_wrapped_block(http, env, &block)
        if (env[:method] == :get) && !env[:body]
          # prefer `get` to `request` because the former handles gzip (ruby 1.9)
          request_via_get_method(http, env, &block)
        else
          request_via_request_method(http, env, &block)
        end
      end

      def request_via_get_method(http, env, &block)
        # Must use Net::HTTP#start and pass it a block otherwise the server's
        # TCP socket does not close correctly.
        http.start do |opened_http|
          opened_http.get env[:url].request_uri, env[:request_headers], &block
        end
      end

      def request_via_request_method(http, env, &block)
        # Must use Net::HTTP#start and pass it a block otherwise the server's
        # TCP socket does not close correctly.
        http.start do |opened_http|
          if block_given?
            opened_http.request create_request(env) do |response|
              response.read_body(&block)
            end
          else
            opened_http.request create_request(env)
          end
        end
      end

      def configure_ssl(http, ssl)
        return unless ssl

        http.verify_mode = ssl_verify_mode(ssl)
        http.cert_store = ssl_cert_store(ssl)

        http.cert = ssl[:client_cert] if ssl[:client_cert]
        http.key = ssl[:client_key] if ssl[:client_key]
        http.ca_file = ssl[:ca_file] if ssl[:ca_file]
        http.ca_path = ssl[:ca_path] if ssl[:ca_path]
        http.verify_depth = ssl[:verify_depth] if ssl[:verify_depth]
        http.ssl_version = ssl[:version] if ssl[:version]
        http.min_version = ssl[:min_version] if ssl[:min_version]
        http.max_version = ssl[:max_version] if ssl[:max_version]
      end

      def configure_request(http, req)
        if (sec = request_timeout(:read, req))
          http.read_timeout = sec
        end

        if (sec = http.respond_to?(:write_timeout=) &&
                  request_timeout(:write, req))
          http.write_timeout = sec
        end

        if (sec = request_timeout(:open, req))
          http.open_timeout = sec
        end

        # Only set if Net::Http supports it, since Ruby 2.5.
        http.max_retries = 0 if http.respond_to?(:max_retries=)

        @config_block&.call(http)
      end

      def ssl_cert_store(ssl)
        return ssl[:cert_store] if ssl[:cert_store]

        @ssl_cert_store ||= begin
          # Use the default cert store by default, i.e. system ca certs
          OpenSSL::X509::Store.new.tap(&:set_default_paths)
        end
      end

      def ssl_verify_mode(ssl)
        ssl[:verify_mode] || begin
          if ssl.fetch(:verify, true)
            OpenSSL::SSL::VERIFY_PEER
          else
            OpenSSL::SSL::VERIFY_NONE
          end
        end
      end
    end
  end
end
