# TODO try stealing pool, each thread has it's own queue

require 'concurrent/promises'

module Concurrent
  module Promises

    class Future < AbstractEventFuture

      # @!macro warn.edge
      module ActorIntegration
        # Asks the actor with its value.
        # @return [Future] new future with the response form the actor
        def then_ask(actor)
          self.then(actor) { |v, a| a.ask_op(v) }.flat
        end
      end

      include ActorIntegration

      # @!macro warn.edge
      module FlatShortcuts

        # @return [Future]
        def then_flat_future(*args, &block)
          self.then(*args, &block).flat_future
        end

        alias_method :then_flat, :then_flat_future

        # @return [Future]
        def then_flat_future_on(executor, *args, &block)
          self.then_on(executor, *args, &block).flat_future
        end

        alias_method :then_flat_on, :then_flat_future_on

        # @return [Event]
        def then_flat_event(*args, &block)
          self.then(*args, &block).flat_event
        end

        # @return [Event]
        def then_flat_event_on(executor, *args, &block)
          self.then_on(executor, *args, &block).flat_event
        end
      end

      include FlatShortcuts
    end

    class Future < AbstractEventFuture
      # @!macro warn.edge
      module NewChannelIntegration

        # @param [Channel] channel to push to.
        # @return [Future] a future which is fulfilled after the message is pushed to the channel.
        #   May take a moment if the channel is full.
        def then_channel_push(channel)
          self.then(channel) { |value, ch| ch.push_op value }.flat_future
        end

      end

      include NewChannelIntegration
    end

    module FactoryMethods
      # @!macro promises.shortcut.on
      # @return [Future]
      # @!macro warn.edge
      def zip_futures_over(enumerable, &future_factory)
        zip_futures_over_on default_executor, enumerable, &future_factory
      end

      # Creates new future which is resolved after all the futures created by future_factory from
      # enumerable elements are resolved. Simplified it does:
      # `zip(*enumerable.map { |e| future e, &future_factory })`
      # @example
      #   # `#succ` calls are executed in parallel
      #   zip_futures_over_on(:io, [1, 2], &:succ).value! # => [2, 3]
      #
      # @!macro promises.param.default_executor
      # @param [Enumerable] enumerable
      # @yield a task to be executed in future
      # @yieldparam [Object] element from enumerable
      # @yieldreturn [Object] a value of the future
      # @return [Future]
      # @!macro warn.edge
      def zip_futures_over_on(default_executor, enumerable, &future_factory)
        # ZipFuturesPromise.new_blocked_by(futures_and_or_events, default_executor).future
        zip_futures_on(default_executor, *enumerable.map { |e| future e, &future_factory })
      end
    end

    module Resolvable
      include InternalStates

      # Reserves the event or future, if reserved others are prevented from resolving it.
      # Advanced feature.
      # Be careful about the order of reservation to avoid deadlocks,
      # the method blocks if the future or event is already reserved
      # until it is released or resolved.
      #
      # @example
      #   f = Concurrent::Promises.resolvable_future
      #   reserved = f.reserve
      #   Thread.new { f.resolve true, :val, nil } # fails
      #   f.resolve true, :val, nil, true if reserved # must be called only if reserved
      # @return [true, false] on successful reservation
      def reserve
        while true
          return true if compare_and_set_internal_state(PENDING, RESERVED)
          return false if resolved?
          # FIXME (pitr-ch 17-Jan-2019): sleep until given up or resolved instead of busy wait
          Thread.pass
        end
      end

      # @return [true, false] on successful release of the reservation
      def release
        compare_and_set_internal_state(RESERVED, PENDING)
      end

      # @return [Comparable] an item to sort the resolvable events or futures
      #   by to get the right global locking order of resolvable events or futures
      # @see .atomic_resolution
      def self.locking_order_by(resolvable)
        resolvable.object_id
      end

      # Resolves all passed events and futures to the given resolutions
      # if possible (all are unresolved) or none.
      #
      # @param [Hash{Resolvable=>resolve_arguments}, Array<Array(Resolvable, resolve_arguments)>] resolvable_map
      #   collection of resolvable events and futures which should be resolved all at once
      #   and what should they be resolved to, examples:
      #   ```ruby
      #   { a_resolvable_future1 => [true, :val, nil],
      #     a_resolvable_future2 => [false, nil, :err],
      #     a_resolvable_event => [] }
      #   ```
      #    or
      #   ```ruby
      #   [[a_resolvable_future1, [true, :val, nil]],
      #    [a_resolvable_future2, [false, nil, :err]],
      #    [a_resolvable_event, []]]
      #   ```
      # @return [true, false] if success
      def self.atomic_resolution(resolvable_map)
        # atomic_resolution event => [], future => [true, :v, nil]
        sorted = resolvable_map.to_a.sort_by { |resolvable, _| locking_order_by resolvable }

        reserved = 0
        while reserved < sorted.size && sorted[reserved].first.reserve
          reserved += 1
        end

        if reserved == sorted.size
          sorted.each { |resolvable, args| resolvable.resolve(*args, true, true) }
          true
        else
          while reserved > 0
            reserved -= 1
            raise 'has to be reserved' unless sorted[reserved].first.release
          end
          false
        end
      end
    end


  end
end
