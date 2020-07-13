require "logstash/pipeline"
require "logstash/java_pipeline"

module LogStash
  class TestPipeline < LogStash::JavaPipeline
    public :flush_filters

    def run_with(events)
      if inputs&.any? # will work but might be unintended
        config = "\n #{config_str}" if $VERBOSE
        warn "#{self} pipeline is getting events pushed manually while having inputs: #{inputs.inspect}  #{config}"
      end
      # TODO could we handle a generator (Enumerator) ?
      queue.write_client.push_batch events.to_a
      queue_read_client = filter_queue_client
      # potentially handle run_with called multiple times (re-use the same instance) :
      if queue_read_client.is_a?(EventTrackingQueueReadClientDelegator)
        queue_read_client.reset_events!
      else
        # start_worker using @filter_queue_client on 6.3, since 6.4 a reader method is used
        # to make things compatible with 6.3 we're assigning the @filter_queue_client ivar
        @filter_queue_client = EventTrackingQueueReadClientDelegator.new queue_read_client
      end
      run
    end

    # @override for WorkerLoop to pick it up
    # @note only works since LS 6.4 (need to use tha actual ivar for 6.3)
    def filter_queue_client
      @filter_queue_client || super
    end

    java_import org.apache.logging.log4j.ThreadContext unless const_defined?(:ThreadContext)

    def start_and_wait
      parent_thread = Thread.current
      @finished_execution.make_false
      @finished_run&.make_false # only since 6.5

      @thread = Thread.new do
        begin
          LogStash::Util.set_thread_name("pipeline.#{pipeline_id}")
          ThreadContext.put("pipeline.id", pipeline_id)
          run
          @finished_run&.make_true
        rescue => e
          close
          parent_thread.raise(e)
        ensure
          @finished_execution.make_true
        end
      end

      unless wait_until_started
        raise "failed to start pipeline: #{self}\n with config: #{config_str.inspect}"
      end

      @thread
    end

    class EventTrackingQueueReadClientDelegator
      include org.logstash.execution.QueueReadClient
      java_import org.logstash.execution.QueueReadClient

      attr_reader :processed_events

      def initialize(delegate)
        # NOTE: can not use LogStash::MemoryReadClient#read_batch due its JavaObject wrapping
        @delegate = delegate.to_java(QueueReadClient)
        @processed_events = []
      end

      # @override QueueBatch readBatch() throws InterruptedException;
      def readBatch
        QueueBatchDelegator.new(self, @delegate.read_batch)
      end

      # @override void closeBatch(QueueBatch batch) throws IOException;
      def closeBatch(batch)
        @delegate.close_batch(batch)
      end

      # @override boolean isEmpty();
      def isEmpty
        @delegate.empty?
      end

      # @override QueueBatch newBatch();
      def newBatch
        @delegate.new_batch
      end

      # @override void startMetrics(QueueBatch batch);
      def startMetrics(batch)
        @delegate.start_metrics(batch)
      end

      # @override void addOutputMetrics(int filteredSize);
      def addOutputMetrics(filteredSize)
        @delegate.add_output_metrics(filteredSize)
      end

      # @override void addFilteredMetrics(int filteredSize);
      def addFilteredMetrics(filteredSize)
        @delegate.add_filtered_metrics(filteredSize)
      end

      # @override
      def set_batch_dimensions(batch_size, batch_delay)
        @delegate.set_batch_dimensions(batch_size, batch_delay)
      end

      # @override
      def close
        @delegate.close
      end

      # @override dispatch to delegate
      def method_missing(method, *args)
        @delegate.public_send(method, *args)
      end

      def filtered_events(events)
        @processed_events.concat(events)
      end

      def reset_events!
        @processed_events = []
      end

    end

    class QueueBatchDelegator
      include org.logstash.execution.QueueBatch

      def initialize(event_tracker, delegate)
        @event_tracker = event_tracker
        @delegate = delegate
      end

      # @override RubyArray to_a();
      def to_a
        @delegate.to_a.tap do |events|
          # filters out rogue (cancelled) events
          @event_tracker.filtered_events events
        end
      end

      # @override int filteredSize();
      def filteredSize
        @delegate.to_java.filtered_size
      end

      # @override void merge(IRubyObject event);
      def merge(event)
        @delegate.merge(event)
      end

      # @override void close() throws IOException;
      def close
        @delegate.close
      end

    end
  end
end
