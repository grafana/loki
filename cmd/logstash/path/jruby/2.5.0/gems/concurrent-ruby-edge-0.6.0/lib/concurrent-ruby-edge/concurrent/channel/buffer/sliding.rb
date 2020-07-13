require 'concurrent/channel/buffer/base'

module Concurrent
  class Channel
    module Buffer

      # A non-blocking, buffered buffer of fixed maximum capacity. When the
      # maximum capacity is reached subsequent {#put} and {#offer} operations
      # will complete and the item will be `put`, but the oldest elements in
      # the buffer will be discarded (not transferred).
      class Sliding < Buffered

        # @!method put(item)
        #   @!macro channel_buffer_put
        #
        #   When the buffer is full, this method will return `true`
        #   immediately and the item will be inserted, but the oldest
        #   elements in the buffer will be discarded (not transferred).

        # @!method offer(item)
        #   @!macro channel_buffer_offer
        #
        #   When the buffer is full, this method will return `true`
        #   immediately and the item will be inserted, but the oldest
        #   elements in the buffer will be discarded (not transferred).

        # @!method full?
        #   @!macro channel_buffer_full_question
        #
        #   Always returns `false`.

        # @!macro channel_buffer_blocking_question
        #
        # Always returns `false`.
        def blocking?
          false
        end

        private

        # @!macro channel_buffer_full_question
        def ns_full?
          false
        end

        # @!macro channel_buffer_put
        def ns_put_onto_buffer(item)
          buffer.shift if buffer.size == capacity
          buffer.push(item)
        end
      end
    end
  end
end
