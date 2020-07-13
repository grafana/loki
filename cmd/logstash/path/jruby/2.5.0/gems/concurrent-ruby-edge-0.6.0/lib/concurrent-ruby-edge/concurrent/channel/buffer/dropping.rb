require 'concurrent/channel/buffer/base'

module Concurrent
  class Channel
    module Buffer

      # A non-blocking, buffered buffer of fixed maximum capacity. When the
      # maximum capacity is reached subsequent {#put} and {#offer} operations
      # will complete but the `put` item will be discarded; no transfer will
      # occur.
      class Dropping < Buffered

        # @!method put(item)
        #   @!macro channel_buffer_put
        #
        #   When the buffer is full, this method will return `true`
        #   immediately but the item will be discarded. The item will *not*
        #   be placed into the buffer (no transfer will occur).

        # @!method offer(item)
        #   @!macro channel_buffer_offer
        #
        #   When the buffer is full, this method will return `true`
        #   immediately but the item will be discarded. The item will *not*
        #   be placed into the buffer (no transfer will occur).

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
          buffer.push(item) unless buffer.size == capacity
        end
      end
    end
  end
end
