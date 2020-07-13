require "thread"

module Stud
  STUD_STOP_REQUESTED = :stud_stop_requested

  # This implementation tries to keep clock more accurately.
  # Prior implementations still permitted skew, where as this one
  # will attempt to correct for skew.
  #
  # The execution patterns of this method should be that
  # the start time of 'block.call' should always be at time T*interval
  def self.interval(time, opts = {}, &block)
    start = Time.now
    while true
      if opts[:sleep_then_run]
        start = sleep_for_interval(time, start)
        break if stop?
        block.call
      else
        block.call
        start = sleep_for_interval(time, start)
        break if stop?
      end
    end # loop forever
  end # def interval

  def interval(time, opts = {}, &block)
    Stud.interval(time, opts, &block)
  end # def interval

  # stop! instructs interval to stop and exit its execution loop before going to
  # sleep between block executions.
  # NOW the tricky part is: this is typically an operation that will be called
  # from another thread than the thread running the interval loop in which case
  # the target parameter must be set to the Thread object which is running the
  # interval loop.
  # Note that the stop logic is compatible with Stud::Task so if interval is run
  # inside a Stud::Task, calling Stud::Task#stop! will stop the interval the same
  # way as calling stop! on the interval itself.
  # @param target [Thread] the target thread to stop, defaut to Thread.current
  def self.stop!(target = Thread.current)
    # setting/getting threalocal var is thread safe in JRuby
    target[STUD_STOP_REQUESTED] = true
    begin
      target.wakeup
    rescue ThreadError => e
      # The thread is dead, so there's nothing to do. 
      # There's no race-free way to detect this sadly
    end
    nil
  end

  # stop? returns true if stop! has been called
  # @param target [Thread] the target thread to check for stop, defaut to Thread.current
  # @return [Boolean] true if the stop! has been called
  def self.stop?(target = Thread.current)
    # setting/getting threalocal var is thread safe in JRuby
    target[STUD_STOP_REQUESTED]
  end

  class << Stud
    # also support Stud.interrupted? for backward compatibility.
    alias_method :interrupted?, :stop?
  end

  # stoppable_sleep will try to sleep for the given duration seconds (which may be any number,
  # including a Float with fractional seconds). an optional stop_condition_block can be supplied
  # to verify for sleep interruption if the block returns a truthy value. if not block is supplied
  # it will check for the Stud.stop? condition. this check will be performed at 1s interval
  # by default or you can supply a different stop_condition_interval.
  #
  # note that to achieve this, stoppable_sleep will actually perform a series of incremental sleeps
  # but will try accurately spend the requested duration period in the overall stoppable_sleep method call.
  # in other words this means that the duration supplied will be accurate for the time spent in
  # the stoppable_sleep method not the actual total time spent in the underlying multiple sleep calls.
  #
  # @param duration [Numeric] sleep time in (fractional) seconds
  # @param stop_condition_interval [Numeric] optional interval in (fractional) seconds to perform the sleep interruption verification, default is 1s
  # @param stop_condition_block [Proc] optional sleep interruption code block that must evaluate to a truthy value, default is to use Stud.stop?
  # @return [Numeric] the actual duration in (fractional) seconds spent in stoppable_sleep
  def self.stoppable_sleep(duration, stop_condition_interval = 1.0, &stop_condition_block)
    sleep_start = Time.now

    # default to using Stud.stop? as the condition block
    stop_condition_block ||= lambda { stop? }

    while (remaining_duration = (duration - (Time.now - sleep_start))) >= stop_condition_interval
      # sleep X if there is more than X remaining to sleep in relation to the loop start time
      sleep(stop_condition_interval)

      return(Time.now - sleep_start) if stop_condition_block.call
    end

    # here we know we have less than 1s reminding to sleep,
    sleep(remaining_duration) if remaining_duration > 0.0

    Time.now - sleep_start
  end

  private

  def self.sleep_for_interval(time, start)
    duration = Time.now - start

    # sleep only if the duration was less than the time interval
    if duration < time
      stoppable_sleep(time - duration)
      start += time
    else
      # duration exceeded interval time, reset the clock and do not sleep.
      start = Time.now
    end
  end

end # module Stud
