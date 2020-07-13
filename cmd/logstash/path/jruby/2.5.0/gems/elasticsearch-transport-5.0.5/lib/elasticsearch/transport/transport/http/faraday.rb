module Elasticsearch
  module Transport
    module Transport
      module HTTP

        # The default transport implementation, using the [_Faraday_](https://rubygems.org/gems/faraday)
        # library for abstracting the HTTP client.
        #
        # @see Transport::Base
        #
        class Faraday
          include Base

          # Performs the request by invoking {Transport::Base#perform_request} with a block.
          #
          # @return [Response]
          # @see    Transport::Base#perform_request
          #
          def perform_request(method, path, params={}, body=nil)
            super do |connection, url|
              headers = connection.connection.headers

              response = connection.connection.run_request \
                method.downcase.to_sym,
                url,
                ( body ? __convert_to_json(body) : nil ),
                headers

              Response.new response.status, response.body, response.headers
            end
          end

          # Builds and returns a connection
          #
          # @return [Connections::Connection]
          #
          def __build_connection(host, options={}, block=nil)
            client = ::Faraday::Connection.new(__full_url(host), options, &block)
            Connections::Connection.new :host => host, :connection => client
          end

          # Returns an array of implementation specific connection errors.
          #
          # @return [Array]
          #
          def host_unreachable_exceptions
            [::Faraday::Error::ConnectionFailed, ::Faraday::Error::TimeoutError]
          end
        end
      end
    end
  end
end
