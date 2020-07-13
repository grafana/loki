require "thread"
require "stud/interval"

module Stud

  # A Task spawns a thread to execute the given block. execution completion and result retrieval is
  # done using the Task#wait method. A Task is run once and the thread exists upon block completion.
  # A task and its underlying thread are not reusable.
  #
  # Task does not provide a mean to force-interrupt a running task, it only provides the #stop!
  # method to signal the task for a stop request. The task or code block can use the #stop? method
  # to check for a stop request. Note that the #stop! and #stop? methods are thread safe.
  class Task
    # provide access to the underlying thread if ever needed.
    attr_reader :thread

    def initialize(*args, &block)
      # A queue to receive the result of the block
      # TODO(sissel): Don't use a queue, just store it in an instance variable.
      @queue = Queue.new

      @thread = Thread.new(@queue, *args) do |queue, *args|
        begin
          result = block.call(*args)
          queue << [:return, result]
        rescue => e
          queue << [:exception, e]
        end
      end # thread
    end # def initialize

    # wait waits for the task thread to complete and return the block return value
    # if the block raises an exception, this exception is propagated in this
    # wait method.
    # @return [Object, Exception] block return value
    def wait
      @thread.join
      reason, result = @queue.pop

      if reason == :exception
        #raise StandardError.new(result)
        raise result
      else
        return result
      end
    end # def wait

    # stop! requests the task to stop. the Thread#wakeup method is also
    # called so that a sleeping task is waked up and has a chance to verify
    # the stop request using the #stop? method. also see Stud.stop!
    def stop!
      Stud.stop!(@thread)
    end

    # stop? returns true if this task stop! has been called
    # See Stud.stop?
    # @return [Boolean] true if the stop! has been called
    def stop?
      Stud.stop?(@thread)
    end
    alias_method :interrupted?, :stop?

  end # class Task
end # module Stud
