module Concurrent
  # A tool managing concurrency level of tasks.
  # The maximum capacity is set in constructor.
  # Each acquire will lower the available capacity and release will increase it.
  # When there is no available capacity the current thread may either be blocked or
  # an event is returned which will be resolved when capacity becomes available.
  #
  # The more common usage of the Throttle is with a proxy executor
  # `a_throttle.on(Concurrent.global_io_executor)`.
  # Anything executed on the proxy executor will be throttled and
  # execute on the given executor. There can be more than one proxy executors.
  # All abstractions which execute tasks have option to specify executor,
  # therefore the proxy executor can be injected to any abstraction
  # throttling its concurrency level.
  #
  # {include:file:docs-source/throttle.out.md}
  #
  # @!macro warn.edge
  class Throttle < Synchronization::Object
    safe_initialization!

    attr_atomic(:capacity)
    private :capacity, :capacity=, :swap_capacity, :compare_and_set_capacity, :update_capacity

    # @return [Integer] The available capacity.
    def available_capacity
      current_capacity = capacity
      current_capacity >= 0 ? current_capacity : 0
    end

    # Create throttle.
    # @param [Integer] capacity How many tasks using this throttle can run at the same time.
    def initialize(capacity)
      super()
      @MaxCapacity            = capacity
      @Queue                  = LockFreeQueue.new
      @executor_cache         = [nil, nil]
      self.capacity = capacity
    end

    # @return [Integer] The maximum capacity.
    def max_capacity
      @MaxCapacity
    end

    # Blocks current thread until there is capacity available in the throttle.
    # The acquired capacity has to be returned to the throttle by calling {#release}.
    # If block is passed then the block is called after the capacity is acquired and
    # it is automatically released after the block is executed.
    #
    # @param [Numeric] timeout the maximum time in second to wait.
    # @yield [] block to execute after the capacity is acquired
    # @return [Object, self, true, false]
    #   * When no timeout and no block it returns self
    #   * When no timeout and with block it returns the result of the block
    #   * When with timeout and no block it returns true when acquired and false when timed out
    #   * When with timeout and with block it returns the result of the block of nil on timing out
    # @see #release
    def acquire(timeout = nil, &block)
      event = acquire_or_event
      if event
        within_timeout = event.wait(timeout)
        # release immediately when acquired later after the timeout since it is unused
        event.on_resolution!(self, &:release) unless within_timeout
      else
        within_timeout = true
      end

      called = false
      if timeout
        if block
          if within_timeout
            called = true
            block.call
          else
            nil
          end
        else
          within_timeout
        end
      else
        if block
          called = true
          block.call
        else
          self
        end
      end
    ensure
      release if called
    end

    # Tries to acquire capacity from the throttle.
    # Returns true when there is capacity available.
    # The acquired capacity has to be returned to the throttle by calling {#release}.
    # @return [true, false]
    # @see #release
    def try_acquire
      while true
        current_capacity = capacity
        if current_capacity > 0
          return true if compare_and_set_capacity(
              current_capacity, current_capacity - 1)
        else
          return false
        end
      end
    end

    # Releases previously acquired capacity back to Throttle.
    # Has to be called exactly once for each acquired capacity.
    # @return [self]
    # @see #acquire_operation, #acquire, #try_acquire
    def release
      while true
        current_capacity = capacity
        if compare_and_set_capacity current_capacity, current_capacity + 1
          if current_capacity < 0
            # release called after trigger which pushed a trigger, busy wait is ok
            Thread.pass until (trigger = @Queue.pop)
            trigger.resolve
          end
          return self
        end
      end
    end

    # @return [String] Short string representation.
    def to_s
      format '%s capacity available %d of %d>', super[0..-2], capacity, @MaxCapacity
    end

    alias_method :inspect, :to_s

    # @!visibility private
    def acquire_or_event
      while true
        current_capacity = capacity
        if compare_and_set_capacity current_capacity, current_capacity - 1
          if current_capacity > 0
            return nil
          else
            event = Promises.resolvable_event
            @Queue.push event
            return event
          end
        end
      end
    end

    include Promises::FactoryMethods

    # @param [ExecutorService] executor
    # @return [ExecutorService] An executor which wraps given executor and allows to post tasks only
    #   as available capacity in the throttle allows.
    # @example throttling future
    #   a_future.then_on(a_throttle.on(:io)) { a_throttled_task }
    def on(executor = Promises::FactoryMethods.default_executor)
      current_executor, current_cache = @executor_cache
      return current_cache if current_executor == executor && current_cache

      if current_executor.nil?
        # cache first proxy
        proxy_executor  = ProxyExecutor.new(self, Concurrent.executor(executor))
        @executor_cache = [executor, proxy_executor]
        return proxy_executor
      else
        # do not cache more than 1 executor
        ProxyExecutor.new(self, Concurrent.executor(executor))
      end
    end

    # Uses executor provided by {#on} therefore
    # all events and futures created using factory methods on this object will be throttled.
    # Overrides {Promises::FactoryMethods#default_executor}.
    #
    # @return [ExecutorService]
    # @see Promises::FactoryMethods#default_executor
    def default_executor
      on(super)
    end

    class ProxyExecutor < Synchronization::Object
      safe_initialization!

      include ExecutorService

      def initialize(throttle, executor)
        super()
        @Throttle = throttle
        @Executor = executor
      end

      def post(*args, &task)
        if (event = @Throttle.acquire_or_event)
          event.on_resolution! { inner_post(*args, &task) }
        else
          inner_post(*args, &task)
        end
      end

      def can_overflow?
        @Executor.can_overflow?
      end

      def serialized?
        @Executor.serialized?
      end

      private

      def inner_post(*arguments, &task)
        @Executor.post(*arguments) do |*args|
          begin
            task.call(*args)
          ensure
            @Throttle.release
          end
        end
      end
    end

    private_constant :ProxyExecutor
  end
end
