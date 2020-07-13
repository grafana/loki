module Stud

  # A class implementing 'retry-on-failure'
  #
  # Example:
  #
  #     Try.new.try(5.times) { your_code }
  #
  # A failure is indicated by any exception being raised.
  # On success, the return value of the block is the return value of the try
  # call.
  #
  # On final failure (ran out of things to try), the last exception is raised.
  class Try
    # An infinite enumerator
    class Forever
      include Enumerable
      def each(&block)
        a = 0
        yield a += 1 while true
      end
    end # class Forever

    FOREVER = Forever.new

    BACKOFF_SCHEDULE = [0.01, 0.02, 0.04, 0.08, 0.16, 0.32, 0.64, 1.28, 2.0]

    DEFAULT_CATCHABLE_EXCEPTIONS = [StandardError]

    # Log a failure.
    #
    # You should override this method if you want a better logger.
    def log_failure(exception, fail_count, message)
      puts "Failed (#{exception}). #{message}"
    end # def log_failure

    # This method is called when a try attempt fails.
    #
    # The default implementation will sleep with exponential backoff up to a
    # maximum of 2 seconds (see BACKOFF_SCHEDULE)
    #
    # exception - the exception causing the failure
    # fail_count - how many times we have failed.
    def failure(exception, fail_count)
      backoff = BACKOFF_SCHEDULE[fail_count] || BACKOFF_SCHEDULE.last
      log_failure(exception, fail_count, "Sleeping for #{backoff}")
      sleep(backoff)
    end # def failure

    # Public: try a block of code until either it succeeds or we give up.
    #
    # enumerable - an Enumerable or omitted/nil, #each is invoked and is tried
    #   that number of times. If this value is omitted or nil, we will try until
    #   success with no limit on the number of tries.
    #
    # exceptions - the type of exceptions to retry, we use `StandardError` by default.
    #
    # Returns the return value of the block once the block succeeds.
    # Raises the last seen exception if we run out of tries.
    #
    # Examples
    #
    #   # Try 10 times to fetch http://google.com/
    #   response = try(10.times) { Net::HTTP.get_response("google.com", "/") }
    #
    #   # Try many times, yielding the value of the enumeration to the block.
    #   # This allows you to try different inputs.
    #   response = try([0, 2, 4, 6]) { |val| 50 / val }
    #   
    #   Output: 
    #   Failed (divided by 0). Retrying in 0.01 seconds...
    #   => 25
    #
    #   # Try forever
    #   return_value = try { ... }
    def try(enumerable=FOREVER, exceptions=DEFAULT_CATCHABLE_EXCEPTIONS, &block)
      if block.arity == 0
        # If the block takes no arguments, give none
        procedure = lambda { |val| return block.call }
      else
        # Otherwise, pass the current 'enumerable' value to the block.
        procedure = lambda { |val| return block.call(val) }
      end

      # Track the last exception so we can reraise it on failure.
      last_exception = nil

      # When 'enumerable' runs out of things, if we still haven't succeeded,
      # we'll reraise
      fail_count = 0
      enumerable.each do |val|
        begin
          # If the 'procedure' (the block, really) succeeds, we'll break 
          # and return the return value of the block. Win!
          return procedure.call(val)
        rescue NoMethodError, NameError
          # Abort immediately on exceptions that are unlikely to recover.
          raise
        rescue *exceptions => exception
          last_exception = exception
          fail_count += 1

          # Note: Since we can't reliably detect the 'end' of an enumeration, we
          # will call 'failure' for the final iteration (if it failed) and sleep
          # even though there's no strong reason to backoff on the last error.
          failure(exception, fail_count)
        end
      end # enumerable.each

      # generally make the exception appear from the 'try' method itself, not from
      # any deeply nested enumeration/begin/etc
      # It is my hope that this makes the backtraces easier to read, not more
      # difficult. If you find this is not the case, please please please let me
      # know.
      last_exception.set_backtrace(StandardError.new.backtrace)
      raise last_exception
    end # def try
  end # class Stud::Try

  TRY = Try.new
  # A simple try method for the common case.
  def try(enumerable=Stud::Try::FOREVER, exceptions=Try::DEFAULT_CATCHABLE_EXCEPTIONS, &block)
    return TRY.try(enumerable, exceptions, &block)
  end # def try

  extend self
end # module Stud
