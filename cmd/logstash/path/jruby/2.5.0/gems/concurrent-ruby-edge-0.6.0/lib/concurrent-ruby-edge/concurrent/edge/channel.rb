# @!macro warn.edge
module Concurrent
  module Promises

    # A first in first out channel that accepts messages with push family of methods and returns
    # messages with pop family of methods.
    # Pop and push operations can be represented as futures, see {#pop_op} and {#push_op}.
    # The capacity of the channel can be limited to support back pressure, use capacity option in {#initialize}.
    # {#pop} method blocks ans {#pop_op} returns pending future if there is no message in the channel.
    # If the capacity is limited the {#push} method blocks and {#push_op} returns pending future.
    #
    # {include:file:docs-source/channel.out.md}
    # @!macro warn.edge
    class Channel < Concurrent::Synchronization::Object

      # TODO (pitr-ch 06-Jan-2019): rename to Conduit?, to be able to place it into Concurrent namespace?
      # TODO (pitr-ch 14-Jan-2019): better documentation, do few examples from go
      # TODO (pitr-ch 12-Dec-2018): implement channel closing,
      #   - as a child class? To also have a channel which cannot be closed.
      # TODO (pitr-ch 26-Dec-2016): replace with lock-free implementation, at least getting a message when available should be lock free same goes for push with space available

      # @!macro channel.warn.blocks
      #   @note This function potentially blocks current thread until it can continue.
      #     Be careful it can deadlock.
      #
      # @!macro channel.param.timeout
      #   @param [Numeric] timeout the maximum time in second to wait.

      safe_initialization!

      # Default capacity of the Channel, makes it accept unlimited number of messages.
      UNLIMITED_CAPACITY = ::Object.new
      UNLIMITED_CAPACITY.singleton_class.class_eval do
        include Comparable

        def <=>(other)
          1
        end

        def to_s
          'unlimited'
        end
      end

      NOTHING = Object.new
      private_constant :NOTHING

      # An object which matches anything (with #===)
      ANY = Object.new.tap do |any|
        def any.===(other)
          true
        end

        def any.to_s
          'ANY'
        end
      end

      # Create channel.
      # @param [Integer, UNLIMITED_CAPACITY] capacity the maximum number of messages which can be stored in the channel.
      def initialize(capacity = UNLIMITED_CAPACITY)
        super()
        @Capacity = capacity
        @Mutex    = Mutex.new
        # TODO (pitr-ch 28-Jan-2019): consider linked lists or other data structures for following attributes, things are being deleted from the middle
        @Probes      = []
        @Messages    = []
        @PendingPush = []
      end

      # Push the message into the channel if there is space available.
      # @param [Object] message
      # @return [true, false]
      def try_push(message)
        @Mutex.synchronize { ns_try_push(message) }
      end

      # Returns future which will fulfill when the message is pushed to the channel.
      # @!macro chanel.operation_wait
      #   If it is later waited on the operation with a timeout e.g.`channel.pop_op.wait(1)`
      #   it will not prevent the channel to fulfill the operation later after the timeout.
      #   The operation has to be either processed later
      #   ```ruby
      #   pop_op = channel.pop_op
      #   if pop_op.wait(1)
      #     process_message pop_op.value
      #   else
      #     pop_op.then { |message| log_unprocessed_message message }
      #   end
      #   ```
      #   or the operation can be prevented from completion after timing out by using
      #   `channel.pop_op.wait(1, [true, nil, nil])`.
      #   It will fulfill the operation on timeout preventing channel from doing the operation,
      #   e.g. popping a message.
      #
      # @param [Object] message
      # @return [ResolvableFuture(self)]
      def push_op(message)
        @Mutex.synchronize do
          if ns_try_push(message)
            Promises.fulfilled_future self
          else
            pushed = Promises.resolvable_future
            @PendingPush.push message, pushed
            return pushed
          end
        end
      end

      # Blocks current thread until the message is pushed into the channel.
      #
      # @!macro channel.warn.blocks
      # @param [Object] message
      # @!macro channel.param.timeout
      # @return [self, true, false] self implies timeout was not used, true implies timeout was used
      #   and it was pushed, false implies it was not pushed within timeout.
      def push(message, timeout = nil)
        pushed_op = @Mutex.synchronize do
          return timeout ? true : self if ns_try_push(message)

          pushed = Promises.resolvable_future
          # TODO (pitr-ch 06-Jan-2019): clear timed out pushes in @PendingPush, null messages
          @PendingPush.push message, pushed
          pushed
        end

        result = pushed_op.wait!(timeout, [true, self, nil])
        result == pushed_op ? self : result
      end

      # @!macro promises.channel.try_pop
      #   Pop a message from the channel if there is one available.
      #   @param [Object] no_value returned when there is no message available
      #   @return [Object, no_value] message or nil when there is no message
      def try_pop(no_value = nil)
        try_pop_matching ANY, no_value
      end

      # @!macro promises.channel.try_pop
      # @!macro promises.channel.param.matcher
      #   @param [#===] matcher only consider message which matches `matcher === a_message`
      def try_pop_matching(matcher, no_value = nil)
        @Mutex.synchronize do
          message = ns_shift_message matcher
          return message if message != NOTHING
          message = ns_consume_pending_push matcher
          return message != NOTHING ? message : no_value
        end
      end

      # @!macro promises.channel.pop_op
      #   Returns a future witch will become fulfilled with a value from the channel when one is available.
      #   @!macro chanel.operation_wait
      #
      #   @param [ResolvableFuture] probe the future which will be fulfilled with a channel value
      #   @return [Future(Object)] the probe, its value will be the message when available.
      def pop_op(probe = Promises.resolvable_future)
        @Mutex.synchronize { ns_pop_op(ANY, probe, false) }
      end

      # @!macro promises.channel.pop_op
      # @!macro promises.channel.param.matcher
      def pop_op_matching(matcher, probe = Promises.resolvable_future)
        @Mutex.synchronize { ns_pop_op(matcher, probe, false) }
      end

      # @!macro promises.channel.pop
      #   Blocks current thread until a message is available in the channel for popping.
      #
      #   @!macro channel.warn.blocks
      #   @!macro channel.param.timeout
      #   @param [Object] timeout_value a value returned by the method when it times out
      #   @return [Object, nil] message or nil when timed out
      def pop(timeout = nil, timeout_value = nil)
        pop_matching ANY, timeout, timeout_value
      end

      # @!macro promises.channel.pop
      # @!macro promises.channel.param.matcher
      def pop_matching(matcher, timeout = nil, timeout_value = nil)
        # TODO (pitr-ch 27-Jan-2019): should it try to match pending pushes if it fails to match in the buffer? Maybe only if the size is zero. It could be surprising if it's used as a throttle it might be expected that it will not pop if buffer is full of messages which di not match, it might it expected it will block until the message is added to the buffer
        # that it returns even if the buffer is full. User might expect that it has to be in the buffer first.
        probe = @Mutex.synchronize do
          message = ns_shift_message matcher
          if message == NOTHING
            message = ns_consume_pending_push matcher
            return message if message != NOTHING
          else
            new_message = ns_consume_pending_push ANY
            @Messages.push new_message unless new_message == NOTHING
            return message
          end

          probe = Promises.resolvable_future
          @Probes.push probe, false, matcher
          probe
        end

        probe.value!(timeout, timeout_value, [true, timeout_value, nil])
      end

      # @!macro promises.channel.peek
      #   Behaves as {#try_pop} but it does not remove the message from the channel
      #   @param [Object] no_value returned when there is no message available
      #   @return [Object, no_value] message or nil when there is no message
      def peek(no_value = nil)
        peek_matching ANY, no_value
      end

      # @!macro promises.channel.peek
      # @!macro promises.channel.param.matcher
      def peek_matching(matcher, no_value = nil)
        @Mutex.synchronize do
          message = ns_shift_message matcher, false
          return message if message != NOTHING
          message = ns_consume_pending_push matcher, false
          return message != NOTHING ? message : no_value
        end
      end

      # @!macro promises.channel.try_select
      #   If message is available in the receiver or any of the provided channels
      #   the channel message pair is returned. If there is no message nil is returned.
      #   The returned channel is the origin of the message.
      #
      #   @param [Channel, ::Array<Channel>] channels
      #   @return [::Array(Channel, Object), nil]
      #     pair [channel, message] if one of the channels is available for reading
      def try_select(channels)
        try_select_matching ANY, channels
      end

      # @!macro promises.channel.try_select
      # @!macro promises.channel.param.matcher
      def try_select_matching(matcher, channels)
        message = nil
        channel = [self, *channels].find do |ch|
          message = ch.try_pop_matching(matcher, NOTHING)
          message != NOTHING
        end
        channel ? [channel, message] : nil
      end

      # @!macro promises.channel.select_op
      #   When message is available in the receiver or any of the provided channels
      #   the future is fulfilled with a channel message pair.
      #   The returned channel is the origin of the message.
      #   @!macro chanel.operation_wait
      #
      #   @param [Channel, ::Array<Channel>] channels
      #   @param [ResolvableFuture] probe the future which will be fulfilled with the message
      #   @return [ResolvableFuture(::Array(Channel, Object))] a future which is fulfilled with
      #     pair [channel, message] when one of the channels is available for reading
      def select_op(channels, probe = Promises.resolvable_future)
        select_op_matching ANY, channels, probe
      end

      # @!macro promises.channel.select_op
      # @!macro promises.channel.param.matcher
      def select_op_matching(matcher, channels, probe = Promises.resolvable_future)
        [self, *channels].each { |ch| ch.partial_select_op matcher, probe }
        probe
      end

      # @!macro promises.channel.select
      #   As {#select_op} but does not return future,
      #   it block current thread instead until there is a message available
      #   in the receiver or in any of the channels.
      #
      #   @!macro channel.warn.blocks
      #   @param [Channel, ::Array<Channel>] channels
      #   @!macro channel.param.timeout
      #   @return [::Array(Channel, Object), nil] message or nil when timed out
      #   @see #select_op
      def select(channels, timeout = nil)
        select_matching ANY, channels, timeout
      end

      # @!macro promises.channel.select
      # @!macro promises.channel.param.matcher
      def select_matching(matcher, channels, timeout = nil)
        probe = select_op_matching(matcher, channels)
        probe.value!(timeout, nil, [true, nil, nil])
      end

      # @return [Integer] The number of messages currently stored in the channel.
      def size
        @Mutex.synchronize { @Messages.size }
      end

      # @return [Integer] Maximum capacity of the Channel.
      def capacity
        @Capacity
      end

      # @return [String] Short string representation.
      def to_s
        format '%s capacity taken %s of %s>', super[0..-2], size, @Capacity
      end

      alias_method :inspect, :to_s

      class << self

        # @see #try_select
        # @return [::Array(Channel, Object)]
        def try_select(channels)
          channels.first.try_select(channels[1..-1])
        end

        # @see #select_op
        # @return [Future(::Array(Channel, Object))]
        def select_op(channels, probe = Promises.resolvable_future)
          channels.first.select_op(channels[1..-1], probe)
        end

        # @see #select
        # @return [::Array(Channel, Object), nil]
        def select(channels, timeout = nil)
          channels.first.select(channels[1..-1], timeout)
        end

        # @see #try_select_matching
        # @return [::Array(Channel, Object)]
        def try_select_matching(matcher, channels)
          channels.first.try_select_matching(matcher, channels[1..-1])
        end

        # @see #select_op_matching
        # @return [Future(::Array(Channel, Object))]
        def select_op_matching(matcher, channels, probe = Promises.resolvable_future)
          channels.first.select_op_matching(matcher, channels[1..-1], probe)
        end

        # @see #select_matching
        # @return [::Array(Channel, Object), nil]
        def select_matching(matcher, channels, timeout = nil)
          channels.first.select_matching(matcher, channels[1..-1], timeout)
        end
      end

      # @!visibility private
      def partial_select_op(matcher, probe)
        @Mutex.synchronize { ns_pop_op(matcher, probe, true) }
      end

      private

      def ns_pop_op(matcher, probe, include_channel)
        message = ns_shift_message matcher

        # got message from buffer
        if message != NOTHING
          if probe.fulfill(include_channel ? [self, message] : message, false)
            new_message = ns_consume_pending_push ANY
            @Messages.push new_message unless new_message == NOTHING
          else
            @Messages.unshift message
          end
          return probe
        end

        # no message in buffer, try to pair with a pending push
        i = 0
        while true
          message, pushed = @PendingPush[i, 2]
          break if pushed.nil?

          if matcher === message
            value = include_channel ? [self, message] : message
            if Promises::Resolvable.atomic_resolution(probe  => [true, value, nil],
                                                      pushed => [true, self, nil])
              @PendingPush[i, 2] = []
              return probe
            end

            if probe.resolved?
              return probe
            end

            # so pushed.resolved? has to be true, remove the push
            @PendingPush[i, 2] = []
          end

          i += 2
        end

        # no push to pair with
        # TODO (pitr-ch 11-Jan-2019): clear up probes when timed out, use callback
        @Probes.push probe, include_channel, matcher if probe.pending?
        return probe
      end

      def ns_consume_pending_push(matcher, remove = true)
        i = 0
        while true
          message, pushed = @PendingPush[i, 2]
          return NOTHING unless pushed

          if matcher === message
            resolved           = pushed.resolved?
            @PendingPush[i, 2] = [] if remove || resolved
            # can fail if timed-out, so try without error
            if remove ? pushed.fulfill(self, false) : !resolved
              # pushed fulfilled so actually push the message
              return message
            end
          end

          i += 2
        end
      end

      def ns_try_push(message)
        i = 0
        while true
          probe, include_channel, matcher = @Probes[i, 3]
          break unless probe
          if matcher === message && probe.fulfill(include_channel ? [self, message] : message, false)
            @Probes[i, 3] = []
            return true
          end
          i += 3
        end

        if @Capacity > @Messages.size
          @Messages.push message
          true
        else
          false
        end
      end

      def ns_shift_message(matcher, remove = true)
        i = 0
        while true
          message = @Messages.fetch(i, NOTHING)
          return NOTHING if message == NOTHING

          if matcher === message
            @Messages.delete_at i if remove
            return message
          end

          i += 1
        end
      end
    end
  end
end
