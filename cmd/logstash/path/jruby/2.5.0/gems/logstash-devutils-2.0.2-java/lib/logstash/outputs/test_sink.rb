# encoding: utf-8
require "logstash/namespace"
require "logstash/outputs/base"
require "logstash/errors"

# This output simply discards (but tracks) received events.
class LogStash::Outputs::TestSink < LogStash::Outputs::Base

  concurrency :shared

  config_name "test_sink"

  # Whether we're tracking events received or simply act as a true sink.
  config :store_events, :validate => :boolean, :default => true
  # Plugin could not release itself (on close) if needed to keep its events around.
  config :release_on_close, :validate => :boolean, :default => true
  # Even poll timeout in milliseconds (for input plugin tests consuming events).
  # HINT: set to 0 (null) to wait forever.
  config :event_poll_timeout, :default => 5 * 1000

  TRACKER = java.util.WeakHashMap.new

  # @override plugin hook
  def register
    TRACKER[self] = @_event_store || init_event_store
  end

  # @override plugin impl
  def receive(event)
    event_store << event if store_events?
  end

  # @override plugin hook
  def close
    TRACKER.delete(self)
    @_event_store = false if release_on_close?
  end

  def store_events?
    !!@store_events
  end

  def release_on_close?
    !!@release_on_close
  end

  # Clears the event store.
  def clear!
    event_store.clear
  end

  # @return [Queue] (enumerable) event store
  def event_store
    if @_event_store.nil?
      warn("#{self} event store not initialized (call plugin.register to initialize)", caller_locations(2))
      return init_event_store
    elsif @_event_store.eql?(false)
      warn("#{self} closed - event store no longer available (release_on_close => false if you need to retain events)", caller_locations(2))
      return nil
    end
    @_event_store
  end

  def init_event_store
    @_event_store = Queue.new(@event_poll_timeout.to_f * 1000)
  end

  private

  def warn(msg, callstack = nil)
    Kernel.warn("#{msg}#{callstack ? " called from #{callstack.first}" : nil}")
  end

  # TODO refactor to java.util.concurrent.ConcurrentLinkedQueue
  # Interestingly, using a ConcurrentLinkedQueue gets specs that depend on pop-ing events
  # from the output (e.g. syslog input plugin) passing fine with the old Ruby pipeline.
  # The Java pipeline seems to reach a shutdown before the input yielded events are to be consumed.

  class Queue < java.util.concurrent.LinkedBlockingQueue

    java_import java.util.concurrent.TimeoutException

    MILLISECONDS = java.util.concurrent.TimeUnit::MILLISECONDS
    private_constant :MILLISECONDS

    def initialize(poll_timeout = nil)
      super()
      @timeout = poll_timeout.to_i
    end

    # Ruby Queue like pop-er with (default) blocking.
    # @see Queue#pop
    def pop(non_block = nil)
      # for compatibility we're making it behave like Ruby's Queue
      return poll if non_block
      @timeout.zero? ? take :
          (poll(@timeout, MILLISECONDS) || timeout!(__method__))
    end
    alias deq pop
    alias shift pop

    private

    def timeout!(op)
      raise TimeoutException.new("#{op.inspect} did not complete (in #{@timeout}ms)")
    end
  end
end