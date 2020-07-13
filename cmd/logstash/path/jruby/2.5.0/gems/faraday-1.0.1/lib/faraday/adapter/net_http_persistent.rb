# frozen_string_literal: true

module Faraday
  class Adapter
    # Net::HTTP::Persistent adapter.
    class NetHttpPersistent < NetHttp
      dependency 'net/http/persistent'

      private

      def net_http_connection(env)
        @cached_connection ||=
          if Net::HTTP::Persistent.instance_method(:initialize)
                                  .parameters.first == %i[key name]
            options = { name: 'Faraday' }
            if @connection_options.key?(:pool_size)
              options[:pool_size] = @connection_options[:pool_size]
            end
            Net::HTTP::Persistent.new(**options)
          else
            Net::HTTP::Persistent.new('Faraday')
          end

        proxy_uri = proxy_uri(env)
        if @cached_connection.proxy_uri != proxy_uri
          @cached_connection.proxy = proxy_uri
        end
        @cached_connection
      end

      def proxy_uri(env)
        proxy_uri = nil
        if (proxy = env[:request][:proxy])
          proxy_uri = if proxy[:uri].is_a?(::URI::HTTP)
                        proxy[:uri].dup
                      else
                        ::URI.parse(proxy[:uri].to_s)
                      end
          proxy_uri.user = proxy_uri.password = nil
          # awful patch for net-http-persistent 2.8
          # not unescaping user/password
          if proxy[:user]
            (class << proxy_uri; self; end).class_eval do
              define_method(:user) { proxy[:user] }
              define_method(:password) { proxy[:password] }
            end
          end
        end
        proxy_uri
      end

      def perform_request(http, env)
        http.request env[:url], create_request(env)
      rescue Errno::ETIMEDOUT => e
        raise Faraday::TimeoutError, e
      rescue Net::HTTP::Persistent::Error => e
        raise Faraday::TimeoutError, e if e.message.include? 'Timeout'

        if e.message.include? 'connection refused'
          raise Faraday::ConnectionFailed, e
        end

        raise
      end

      SSL_CONFIGURATIONS = {
        certificate: :client_cert,
        private_key: :client_key,
        ca_file: :ca_file,
        ssl_version: :version,
        min_version: :min_version,
        max_version: :max_version
      }.freeze

      def configure_ssl(http, ssl)
        return unless ssl

        http_set(http, :verify_mode, ssl_verify_mode(ssl))
        http_set(http, :cert_store, ssl_cert_store(ssl))

        SSL_CONFIGURATIONS
          .select { |_, key| ssl[key] }
          .each { |target, key| http_set(http, target, ssl[key]) }
      end

      def http_set(http, attr, value)
        http.send("#{attr}=", value) if http.send(attr) != value
      end
    end
  end
end
