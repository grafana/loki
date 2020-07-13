require 'concurrent/constants'
require 'concurrent/channel/buffer/base'

module Concurrent
  class Channel
    module Buffer

      # A buffer with a fixed internal capacity. Items can be put onto the
      # buffer without blocking until the internal capacity is reached. Once
      # the buffer is at capacity, subsequent calls to {#put} will block until
      # an item is removed from the buffer, creating spare capacity.
      class Buffered < Base

        # @!macro channel_buffer_put
        #
        # New items can be put onto the buffer until the number of items in
        # the buffer reaches the {#size} value specified during
        # initialization.
        def put(item)
          loop do
            synchronize do
              if ns_closed?
                return false
              elsif !ns_full?
                ns_put_onto_buffer(item)
                return true
              end
            end
            Thread.pass
          end
        end

        # @!macro channel_buffer_offer
        #
        # New items can be put onto the buffer until the number of items in
        # the buffer reaches the {#size} value specified during
        # initialization.
        def offer(item)
          synchronize do
            if ns_closed? || ns_full?
              return false
            else
              ns_put_onto_buffer(item)
              return true
            end
          end
        end

        # @!macro channel_buffer_take
        def take
          item, _ = self.next
          item
        end

        # @!macro channel_buffer_next
        def next
          loop do
            synchronize do
              if ns_closed? && ns_empty?
                return Concurrent::NULL, false
              elsif !ns_empty?
                item = buffer.shift
                return item, true
              end
            end
            Thread.pass
          end
        end

        # @!macro channel_buffer_poll
        def poll
          synchronize do
            if ns_empty?
              Concurrent::NULL
            else
              buffer.shift
            end
          end
        end

        private

        # @!macro channel_buffer_initialize
        #
        # @param [Integer] size the maximum capacity of the buffer; must be
        #   greater than zero.
        # @raise [ArgumentError] when the size is zero (0) or less.
        def ns_initialize(size)
          raise ArgumentError.new('size must be greater than 0') if size.to_i <= 0
          self.capacity = size.to_i
          self.buffer = []
        end

        # @!macro channel_buffer_size_reader
        def ns_size
          buffer.size
        end

        # @!macro channel_buffer_empty_question
        def ns_empty?
          ns_size == 0
        end

        # @!macro channel_buffer_full_question
        def ns_full?
          ns_size == capacity
        end

        # @!macro channel_buffer_put
        def ns_put_onto_buffer(item)
          buffer.push(item)
        end
      end
    end
  end
end
