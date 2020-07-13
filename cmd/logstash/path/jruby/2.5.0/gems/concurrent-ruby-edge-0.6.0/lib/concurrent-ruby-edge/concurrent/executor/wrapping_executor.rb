module Concurrent

  # A delegating executor which modifies each task with arguments
  # before the task is given to the target executor it delegates to.
  # @example Count task executions
  #   counter          = AtomicFixnum.new
  #   count_executions = WrappingExecutor.new Concurrent.global_io_executor do |*args, &task|
  #     [*args, -> *args { counter.increment; task.call *args }]
  #   end
  #   10.times { count_executions.post { :do_something } }
  #   sleep 0.01
  #   counter.value #=> 10
  class WrappingExecutor < Synchronization::Object
    safe_initialization!

    include ExecutorService

    # @param [Executor] executor an executor to delegate the tasks to
    # @yield [*args, &task] A function which can modify the task with arguments
    # @yieldparam [Array<Object>] *args the arguments submitted with the tasks
    # @yieldparam [block] &task the task submitted to the executor to be modified
    # @yieldreturn [Array<Object>] a new arguments and task `[*args, task]` which are submitted to the target executor
    def initialize(executor, &wrapper)
      super()
      @Wrapper  = wrapper
      @Executor = executor
    end

    # @!macro executor_service_method_post
    #
    # @see #initialize how the tasks can be modified
    def post(*args, &task)
      *args, task = @Wrapper.call(*args, &task)
      @Executor.post(*args, &task)
    end

    # @!macro executor_service_method_can_overflow_question
    def can_overflow?
      @Executor.can_overflow?
    end

    # @!macro executor_service_method_serialized_question
    def serialized?
      @Executor.serialized?
    end
  end
end
