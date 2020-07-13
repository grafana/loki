module Elasticsearch
  module Transport
    module Transport
      module Connections

        # Wraps the collection of connections for the transport object as an Enumerable object.
        #
        # @see Base#connections
        # @see Selector::Base#select
        # @see Connection
        #
        class Collection
          include Enumerable

          DEFAULT_SELECTOR = Selector::RoundRobin

          attr_reader :selector

          # @option arguments [Array]    :connections    An array of {Connection} objects.
          # @option arguments [Constant] :selector_class The class to be used as a connection selector strategy.
          # @option arguments [Object]   :selector       The selector strategy object.
          #
          def initialize(arguments={})
            selector_class = arguments[:selector_class] || DEFAULT_SELECTOR
            @connections   = arguments[:connections]    || []
            @selector      = arguments[:selector]       || selector_class.new(arguments.merge(:connections => self))
          end

          # Returns an Array of hosts information in this collection as Hashes.
          #
          # @return [Array]
          #
          def hosts
            @connections.to_a.map { |c| c.host }
          end

          # Returns an Array of alive connections.
          #
          # @return [Array]
          #
          def connections
            @connections.reject { |c| c.dead? }
          end
          alias :alive :connections

          # Returns an Array of dead connections.
          #
          # @return [Array]
          #
          def dead
            @connections.select { |c| c.dead? }
          end

          # Returns an Array of all connections, both dead and alive
          #
          # @return [Array]
          #
          def all
            @connections
          end

          # Returns a connection.
          #
          # If there are no alive connections, resurrects a connection with least failures.
          # Delegates to selector's `#select` method to get the connection.
          #
          # @return [Connection]
          #
          def get_connection(options={})
            if connections.empty? && dead_connection = dead.sort { |a,b| a.failures <=> b.failures }.first
              dead_connection.alive!
            end
            selector.select(options)
          end

          def each(&block)
            connections.each(&block)
          end

          def slice(*args)
            connections.slice(*args)
          end
          alias :[] :slice

          def size
            connections.size
          end

          # Add connection(s) to the collection
          #
          # @param connections [Connection,Array] A connection or an array of connections to add
          # @return [self]
          #
          def add(connections)
            @connections += Array(connections).to_a
            self
          end

          # Remove connection(s) from the collection
          #
          # @param connections [Connection,Array] A connection or an array of connections to remove
          # @return [self]
          #
          def remove(connections)
            @connections -= Array(connections).to_a
            self
          end
        end

      end
    end
  end
end
