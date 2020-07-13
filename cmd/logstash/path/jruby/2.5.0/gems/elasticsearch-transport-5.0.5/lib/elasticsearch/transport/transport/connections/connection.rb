module Elasticsearch
  module Transport
    module Transport
      module Connections

        # Wraps the connection information and logic.
        #
        # The Connection instance wraps the host information (hostname, port, attributes, etc),
        # as well as the "session" (a transport client object, such as a {HTTP::Faraday} instance).
        #
        # It provides methods to construct and properly encode the URLs and paths for passing them
        # to the transport client object.
        #
        # It provides methods to handle connection livecycle (dead, alive, healthy).
        #
        class Connection
          DEFAULT_RESURRECT_TIMEOUT = 60

          attr_reader :host, :connection, :options, :failures, :dead_since

          # @option arguments [Hash]   :host       Host information (example: `{host: 'localhost', port: 9200}`)
          # @option arguments [Object] :connection The transport-specific physical connection or "session"
          # @option arguments [Hash]   :options    Options (usually passed in from transport)
          #
          def initialize(arguments={})
            @host       = arguments[:host]
            @connection = arguments[:connection]
            @options    = arguments[:options] || {}
            @state_mutex = Mutex.new

            @options[:resurrect_timeout] ||= DEFAULT_RESURRECT_TIMEOUT
            @dead = false
            @failures = 0
          end

          # Returns the complete endpoint URL with host, port, path and serialized parameters.
          #
          # @return [String]
          #
          def full_url(path, params={})
            url  = "#{host[:protocol]}://"
            url += "#{CGI.escape(host[:user])}:#{CGI.escape(host[:password])}@" if host[:user]
            url += "#{host[:host]}:#{host[:port]}"
            url += "#{host[:path]}" if host[:path]
            url += "/#{full_path(path, params)}"
          end

          # Returns the complete endpoint path with serialized parameters.
          #
          # @return [String]
          #
          def full_path(path, params={})
            path + (params.empty? ? '' : "?#{::Faraday::Utils::ParamsHash[params].to_query}")
          end

          # Returns true when this connection has been marked dead, false otherwise.
          #
          # @return [Boolean]
          #
          def dead?
            @dead || false
          end

          # Marks this connection as dead, incrementing the `failures` counter and
          # storing the current time as `dead_since`.
          #
          # @return [self]
          #
          def dead!
            @state_mutex.synchronize do
              @dead       = true
              @failures  += 1
              @dead_since = Time.now
            end
            self
          end

          # Marks this connection as alive, ie. it is eligible to be returned from the pool by the selector.
          #
          # @return [self]
          #
          def alive!
            @state_mutex.synchronize do
              @dead     = false
            end
            self
          end

          # Marks this connection as healthy, ie. a request has been successfully performed with it.
          #
          # @return [self]
          #
          def healthy!
            @state_mutex.synchronize do
              @dead     = false
              @failures = 0
            end
            self
          end

          # Marks this connection as alive, if the required timeout has passed.
          #
          # @return [self,nil]
          # @see    DEFAULT_RESURRECT_TIMEOUT
          # @see    #resurrectable?
          #
          def resurrect!
            alive! if resurrectable?
          end

          # Returns true if the connection is eligible to be resurrected as alive, false otherwise.
          #
          # @return [Boolean]
          #
          def resurrectable?
            @state_mutex.synchronize {
              Time.now > @dead_since + ( @options[:resurrect_timeout] * 2 ** (@failures-1) )
            }
          end

          # Equality operator based on connection protocol, host and port
          #
          # @return [Boolean]
          #
          def ==(other)
            self.host[:protocol] == other.host[:protocol] && \
            self.host[:host] == other.host[:host] && \
            self.host[:port].to_i == other.host[:port].to_i
          end

          # @return [String]
          #
          def to_s
            "<#{self.class.name} host: #{host} (#{dead? ? 'dead since ' + dead_since.to_s : 'alive'})>"
          end
        end

      end
    end
  end
end
