require 'concurrent/constants'
require 'concurrent/channel/buffer/base'
require 'concurrent/atomic/atomic_reference'

module Concurrent
  class Channel
    module Buffer

      # A blocking buffer with a size of zero. An item can only be put onto
      # the buffer when a thread is waiting to take. Similarly, an item can
      # only be put onto the buffer when a thread is waiting to put. When
      # either {#put} or {#take} is called and there is no corresponding call
      # in progress, the call will block indefinitely. Any other calls to the
      # same method will queue behind the first call and block as well. As
      # soon as a corresponding put/take call is made an exchange will occur
      # and the first blocked call will return.
      class Unbuffered < Base

        # @!macro channel_buffer_size_reader
        def size
          synchronize do
            putting.empty? ? 0 : 1
          end
        end

        # @!macro channel_buffer_empty_question
        def empty?
          size == 0
        end

        # @!macro channel_buffer_full_question
        def full?
          !empty?
        end

        # @!macro channel_buffer_put
        #
        # Items can only be put onto the buffer when one or more threads are
        # waiting to {#take} items off the buffer. When there is a thread
        # waiting to take an item this method will give its item and return
        # immediately. When there are no threads waiting to take, this method
        # will block. As soon as a thread calls `take` the exchange will
        # occur and this method will return.
        def put(item)
          mine = synchronize do
            return false if ns_closed?

            ref = Concurrent::AtomicReference.new(item)
            if taking.empty?
              putting.push(ref)
            else
              taken = taking.shift
              taken.value = item
              ref.value = nil
            end
            ref
          end
          loop do
            return true if mine.value.nil?
            Thread.pass
          end
        end

        # @!macro channel_buffer_offer
        #
        # Items can only be put onto the buffer when one or more threads are
        # waiting to {#take} items off the buffer. When there is a thread
        # waiting to take an item this method will give its item and return
        # `true` immediately. When there are no threads waiting to take or the
        # buffer is closed, this method will return `false` immediately.
        def offer(item)
          synchronize do
            return false if ns_closed? || taking.empty?

            taken = taking.shift
            taken.value = item
            true
          end
        end

        # @!macro channel_buffer_take
        #
        # Items can only be taken from the buffer when one or more threads are
        # waiting to {#put} items onto the buffer. When there is a thread
        # waiting to put an item this method will take that item and return it
        # immediately. When there are no threads waiting to put, this method
        # will block. As soon as a thread calls `pur` the exchange will occur
        # and this method will return.
        def take
          mine = synchronize do
            return Concurrent::NULL if ns_closed? && putting.empty?

            ref = Concurrent::AtomicReference.new(nil)
            if putting.empty?
              taking.push(ref)
            else
              put = putting.shift
              ref.value = put.value
              put.value = nil
            end
            ref
          end
          loop do
            item = mine.value
            return item if item
            Thread.pass
          end
        end

        # @!macro channel_buffer_poll
        #
        # Items can only be taken off the buffer when one or more threads are
        # waiting to {#put} items onto the buffer. When there is a thread
        # waiting to put an item this method will take the item and return
        # it immediately. When there are no threads waiting to put or the
        # buffer is closed, this method will return `Concurrent::NULL` immediately.
        def poll
          synchronize do
            return Concurrent::NULL if putting.empty?

            put = putting.shift
            value = put.value
            put.value = nil
            value
          end
        end

        # @!macro channel_buffer_next
        #
        # Items can only be taken from the buffer when one or more threads are
        # waiting to {#put} items onto the buffer. This method exhibits the
        # same blocking behavior as {#take}.
        #
        # @see #take
        def next
          item = take
          more = (item != Concurrent::NULL)
          return item, more
        end

        private

        def putting() @putting; end

        def taking() @taking; end

        # @!macro channel_buffer_initialize
        def ns_initialize
          # one will always be empty
          @putting = []
          @taking = []
          self.closed = false
          self.capacity = 1
        end
      end
    end
  end
end
