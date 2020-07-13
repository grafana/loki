require 'concurrent/synchronization/lockable_object'

module Concurrent
  class Channel
    module Buffer

      # Abstract base class for all Channel buffers.
      #
      # {Concurrent::Channel} objects maintain an internal, queue-like
      # object called a buffer. It's the storage bin for values put onto or
      # taken from the channel. Different buffer types have different
      # characteristics. Subsequently, the behavior of any given channel is
      # highly dependent uping the type of its buffer. This is the base class
      # which defines the common buffer interface. Any class intended to be
      # used as a channel buffer should extend this class.
      class Base < Synchronization::LockableObject

        # @!macro channel_buffer_capacity_reader
        #
        #   The maximum number of values which can be {#put} onto the buffer
        #   it becomes full.
        attr_reader :capacity

        # @!macro channel_buffer_initialize
        #
        #   Creates a new buffer.
        def initialize(*args)
          super()
          synchronize do
            @closed = false
            @size = 0
            @capacity = 0
            @buffer = nil
            ns_initialize(*args)
          end
        end

        # @!macro channel_buffer_blocking_question
        #
        #   Predicate indicating if this buffer will block {#put} operations
        #   once it reaches its maximum capacity.
        #
        #   @return [Boolean] true if this buffer blocks else false
        def blocking?
          true
        end

        # @!macro channel_buffer_size_reader
        #
        #   The number of items currently in the buffer.
        def size
          synchronize { ns_size }
        end

        # @!macro channel_buffer_empty_question
        #
        #   Predicate indicating if the buffer is empty.
        #
        #   @return [Boolean] true if this buffer is empty else false
        #
        # @raise [NotImplementedError] until overridden in a subclass.
        def empty?
          synchronize { ns_empty? }
        end

        # @!macro channel_buffer_full_question
        #
        #   Predicate indicating if the buffer is full.
        #
        #   @return [Boolean] true if this buffer is full else false
        #
        # @raise [NotImplementedError] until overridden in a subclass.
        def full?
          synchronize { ns_full? }
        end

        # @!macro channel_buffer_put
        #
        #   Put an item onto the buffer if possible. If the buffer is open
        #   but not able to accept the item the calling thread will block
        #   until the item can be put onto the buffer.
        #
        #   @param [Object] item the item/value to put onto the buffer.
        #   @return [Boolean] true if the item was added to the buffer else
        #     false (always false when closed).
        #
        # @raise [NotImplementedError] until overridden in a subclass.
        def put(item)
          raise NotImplementedError
        end

        # @!macro channel_buffer_offer
        #
        #   Put an item onto the buffer if possible. If the buffer is open but
        #   unable to add an item, probably due to being full, the method will
        #   return immediately. Similarly, the method will return immediately
        #   when the buffer is closed. A return value of `false` does not
        #   necessarily indicate that the buffer is closed, just that the item
        #   could not be added.
        #
        #   @param [Object] item the item/value to put onto the buffer.
        #   @return [Boolean] true if the item was added to the buffer else
        #     false (always false when closed).
        #
        # @raise [NotImplementedError] until overridden in a subclass.
        def offer(item)
          raise NotImplementedError
        end

        # @!macro channel_buffer_take
        #
        #   Take an item from the buffer if one is available. If the buffer
        #   is open and no item is available the calling thread will block
        #   until an item is available. If the buffer is closed but items
        #   are available the remaining items can still be taken. Once the
        #   buffer closes, no remaining items can be taken.
        #
        #   @return [Object] the item removed from the buffer; `Concurrent::NULL` once
        #     the buffer has closed.
        #
        # @raise [NotImplementedError] until overridden in a subclass.
        def take
          raise NotImplementedError
        end

        # @!macro channel_buffer_next
        #
        #   Take the next "item" from the buffer and also return a boolean
        #   indicating if "more" items can be taken. Used for iterating
        #   over a buffer until it is closed and empty.
        #
        #   If the buffer is open but no items remain the calling thread will
        #   block until an item is available. The second of the two return
        #   values, "more" (a boolean), will always be `true` when the buffer is
        #   open. The "more" value will be `false` when the channel has been
        #   closed and all values have already been received. When "more" is
        #   false the returned item will be `Concurrent::NULL`.
        #
        #   Note that when multiple threads access the same channel a race
        #   condition can occur when using this method. A call to `next` from
        #   one thread may return `true` for the second return value, but
        #   another thread may `take` the last value before the original
        #   thread makes another call. Code which iterates over a channel
        #   must be programmed to properly handle these race conditions.
        #
        #   @return [Object, Boolean] the first return value will be the item
        #     taken from the buffer and the second return value will be a
        #     boolean indicating whether or not more items remain.
        #
        # @raise [NotImplementedError] until overridden in a subclass.
        def next
          raise NotImplementedError
        end

        # @!macro channel_buffer_poll
        #
        #   Take the next item from the buffer if one is available else return
        #   immediately. Failing to return a value does not necessarily
        #   indicate that the buffer is closed, just that it is empty.
        #
        #   @return [Object] the next item from the buffer or `Concurrent::NULL` if
        #     the buffer is empty.
        #
        # @raise [NotImplementedError] until overridden in a subclass.
        def poll
          raise NotImplementedError
        end

        # @!macro channel_buffer_close
        #
        #   Close the buffer, preventing new items from being added. Once a
        #   buffer is closed it cannot be opened again.
        #
        #   @return [Boolean] true if the buffer was open and successfully
        #     closed else false.
        def close
          synchronize do
            @closed ? false : @closed = true
          end
        end

        # @!macro channel_buffer_closed_question
        #
        #   Predicate indicating is this buffer closed.
        #
        #   @return [Boolea] true when closed else false.
        def closed?
          synchronize { ns_closed? }
        end

        private

        def buffer
          @buffer
        end

        def buffer=(value)
          @buffer = value
        end

        def closed=(value)
          @closed = value
        end

        def capacity=(value)
          @capacity = value
        end

        def size=(value)
          @size = value
        end

        def ns_initialize(*args)
        end

        # @!macro channel_buffer_size_reader
        def ns_size
          raise NotImplementedError
        end

        # @!macro channel_buffer_empty_question
        def ns_empty?
          raise NotImplementedError
        end

        # @!macro channel_buffer_full_question
        def ns_full?
          raise NotImplementedError
        end

        # @!macro channel_buffer_closed_question
        def ns_closed?
          @closed
        end
      end
    end
  end
end
