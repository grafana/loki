module Elasticsearch
  module Transport
    module Transport
      module HTTP

        # Alternative HTTP transport implementation, using the [_Curb_](https://rubygems.org/gems/curb) client.
        #
        # @see Transport::Base
        #
        class Curb
          include Base

          # Performs the request by invoking {Transport::Base#perform_request} with a block.
          #
          # @return [Response]
          # @see    Transport::Base#perform_request
          #
          def perform_request(method, path, params={}, body=nil)
            super do |connection,url|
              connection.connection.url = url

              case method
                when 'HEAD'
                  connection.connection.set :nobody, true
                when 'GET', 'POST', 'PUT', 'DELETE'
                  connection.connection.set :nobody, false

                  connection.connection.put_data = __convert_to_json(body) if body
                else raise ArgumentError, "Unsupported HTTP method: #{method}"
              end

              connection.connection.http(method.to_sym)

              response_headers = {}
              response_headers['content-type'] = 'application/json' if connection.connection.header_str =~ /\/json/

              Response.new connection.connection.response_code,
                           connection.connection.body_str,
                           response_headers
            end
          end

          # Builds and returns a connection
          #
          # @return [Connections::Connection]
          #
          def __build_connection(host, options={}, block=nil)
            client = ::Curl::Easy.new

            headers = options[:headers] || {}
            headers.update('User-Agent' => "Curb #{Curl::CURB_VERSION}")

            client.headers = headers
            client.url     = __full_url(host)

            if host[:user]
              client.http_auth_types = host[:auth_type] || :basic
              client.username = host[:user]
              client.password = host[:password]
            end

            client.instance_eval(&block) if block

            Connections::Connection.new :host => host, :connection => client
          end

          # Returns an array of implementation specific connection errors.
          #
          # @return [Array]
          #
          def host_unreachable_exceptions
            [
              ::Curl::Err::HostResolutionError,
              ::Curl::Err::ConnectionFailedError,
              ::Curl::Err::GotNothingError,
              ::Curl::Err::RecvError,
              ::Curl::Err::SendError,
              ::Curl::Err::TimeoutError
            ]
          end
        end

      end
    end
  end
end
