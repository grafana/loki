require 'manticore'

module Elasticsearch
  module Transport
    module Transport
      module HTTP
        # Alternative HTTP transport implementation for JRuby,
        # using the [_Manticore_](https://github.com/cheald/manticore) client,
        #
        # @example HTTP
        #
        #     require 'elasticsearch/transport/transport/http/manticore'
        #
        #     client = Elasticsearch::Client.new transport_class: Elasticsearch::Transport::Transport::HTTP::Manticore
        #
        #     client.transport.connections.first.connection
        #     => #<Manticore::Client:0x56bf7ca6 ...>
        #
        #     client.info['status']
        #     => 200
        #
        #  @example HTTPS (All SSL settings are optional,
        #                  see http://www.rubydoc.info/gems/manticore/Manticore/Client:initialize)
        #
        #     require 'elasticsearch/transport/transport/http/manticore'
        #
        #     client = Elasticsearch::Client.new \
        #       url: 'https://elasticsearch.example.com',
        #       transport_class: Elasticsearch::Transport::Transport::HTTP::Manticore,
        #       ssl: {
        #         truststore: '/tmp/truststore.jks',
        #         truststore_password: 'password',
        #         keystore: '/tmp/keystore.jks',
        #         keystore_password: 'secret',
        #       }
        #
        #     client.transport.connections.first.connection
        #     => #<Manticore::Client:0xdeadbeef ...>
        #
        #     client.info['status']
        #     => 200
        #
        # @see Transport::Base
        #
        class Manticore
          include Base

          def initialize(arguments={}, &block)
            @manticore = build_client(arguments[:options] || {})
            super(arguments, &block)
          end

          # Should just be run once at startup
          def build_client(options={})
            client_options = options[:transport_options] || {}
            client_options[:ssl] = options[:ssl] || {}

            @manticore = ::Manticore::Client.new(client_options)
          end

          # Performs the request by invoking {Transport::Base#perform_request} with a block.
          #
          # @return [Response]
          # @see    Transport::Base#perform_request
          #
          def perform_request(method, path, params={}, body=nil)
            super do |connection, url|
              params[:body] = __convert_to_json(body) if body
              params = params.merge @request_options
              case method
              when "GET"
                resp = connection.connection.get(url, params)
              when "HEAD"
                resp = connection.connection.head(url, params)
              when "PUT"
                resp = connection.connection.put(url, params)
              when "POST"
                resp = connection.connection.post(url, params)
              when "DELETE"
                resp = connection.connection.delete(url, params)
              else
                raise ArgumentError.new "Method #{method} not supported"
              end
              Response.new resp.code, resp.read_body, resp.headers
            end
          end

          # Builds and returns a collection of connections.
          # Each connection is a Manticore::Client
          #
          # @return [Connections::Collection]
          #
          def __build_connections
            @request_options = {}

            if options[:transport_options] && options[:transport_options][:headers]
              @request_options[:headers] = options[:transport_options][:headers]
            end

            if options.key?(:headers)
              @request_options[:headers] = options[:headers]
            end

            Connections::Collection.new \
              :connections => hosts.map { |host|
                host[:protocol]   = host[:scheme] || DEFAULT_PROTOCOL
                host[:port]     ||= DEFAULT_PORT

                host.delete(:user)     # auth is not supported here.
                host.delete(:password) # use the headers

                Connections::Connection.new \
                  :host => host,
                  :connection => @manticore
              },
              :selector_class => options[:selector_class],
              :selector => options[:selector]
          end

          # Closes all connections by marking them as dead
          # and closing the underlying HttpClient instances
          #
          # @return [Connections::Collection]
          #
          def __close_connections
            # The Manticore adapter uses a single long-lived instance
            # of Manticore::Client, so we don't close the connections.
          end

          # Returns an array of implementation specific connection errors.
          #
          # @return [Array]
          #
          def host_unreachable_exceptions
            [
              ::Manticore::Timeout,
              ::Manticore::SocketException,
              ::Manticore::ClientProtocolException,
              ::Manticore::ResolutionFailure
            ]
          end
        end
      end
    end
  end
end
