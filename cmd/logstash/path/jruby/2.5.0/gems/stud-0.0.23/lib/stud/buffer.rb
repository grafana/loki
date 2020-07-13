module Stud

  # @author {Alex Dean}[http://github.com/alexdean]
  #
  # Implements a generic framework for accepting events which are later flushed
  # in batches. Flushing occurs whenever +:max_items+ or +:max_interval+ (seconds)
  # has been reached.
  #
  # Including class must implement +flush+, which will be called with all
  # accumulated items either when the output buffer fills (+:max_items+) or
  # when a fixed amount of time (+:max_interval+) passes.
  #
  # == batch_receive and flush
  # General receive/flush can be implemented in one of two ways.
  #
  # === batch_receive(event) / flush(events)
  # +flush+ will receive an array of events which were passed to +buffer_receive+.
  #
  #   batch_receive('one')
  #   batch_receive('two')
  #
  # will cause a flush invocation like
  #
  #   flush(['one', 'two'])
  #
  # === batch_receive(event, group) / flush(events, group)
  # flush() will receive an array of events, plus a grouping key.
  #
  #   batch_receive('one',   :server => 'a')
  #   batch_receive('two',   :server => 'b')
  #   batch_receive('three', :server => 'a')
  #   batch_receive('four',  :server => 'b')
  #
  # will result in the following flush calls
  #
  #   flush(['one', 'three'], {:server => 'a'})
  #   flush(['two', 'four'],  {:server => 'b'})
  #
  # Grouping keys can be anything which are valid Hash keys. (They don't have to
  # be hashes themselves.) Strings or Fixnums work fine. Use anything which you'd
  # like to receive in your +flush+ method to help enable different handling for
  # various groups of events.
  #
  # == on_flush_error
  # Including class may implement +on_flush_error+, which will be called with an
  # Exception instance whenever buffer_flush encounters an error.
  #
  # * +buffer_flush+ will automatically re-try failed flushes, so +on_flush_error+
  #   should not try to implement retry behavior.
  # * Exceptions occurring within +on_flush_error+ are not handled by
  #   +buffer_flush+.
  #
  # == on_full_buffer_receive
  # Including class may implement +on_full_buffer_receive+, which will be called
  # whenever +buffer_receive+ is called while the buffer is full.
  #
  # +on_full_buffer_receive+ will receive a Hash like <code>{:pending => 30,
  # :outgoing => 20}</code> which describes the internal state of the module at
  # the moment.
  #
  # == final flush
  # Including class should call <code>buffer_flush(:final => true)</code>
  # during a teardown/shutdown routine (after the last call to buffer_receive)
  # to ensure that all accumulated messages are flushed.
  module Buffer

    public
    # Initialize the buffer.
    #
    # Call directly from your constructor if you wish to set some non-default
    # options. Otherwise buffer_initialize will be called automatically during the
    # first buffer_receive call.
    #
    # Options:
    # * :max_items, Max number of items to buffer before flushing. Default 50.
    # * :max_interval, Max number of seconds to wait between flushes. Default 5.
    # * :logger, A logger to write log messages to. No default. Optional.
    #
    # @param [Hash] options
    def buffer_initialize(options={})
      if ! self.class.method_defined?(:flush)
        raise ArgumentError, "Any class including Stud::Buffer must define a flush() method."
      end

      @buffer_config = {
        :max_items => options[:max_items] || 50,
        :max_interval => options[:max_interval] || 5,
        :logger => options[:logger] || nil,
        :has_on_flush_error => self.class.method_defined?(:on_flush_error),
        :has_on_full_buffer_receive => self.class.method_defined?(:on_full_buffer_receive)
      }
      @buffer_state = {
        # items accepted from including class
        :pending_items => {},
        :pending_count => 0,

        # guard access to pending_items & pending_count
        :pending_mutex => Mutex.new,

        # items which are currently being flushed
        :outgoing_items => {},
        :outgoing_count => 0,

        # ensure only 1 flush is operating at once
        :flush_mutex => Mutex.new,

        # data for timed flushes
        :last_flush => Time.now,
        :timer => Thread.new do
          loop do
            sleep(@buffer_config[:max_interval])
            buffer_flush(:force => true)
          end
        end
      }

      # events we've accumulated
      buffer_clear_pending
    end

    # Determine if +:max_items+ has been reached.
    #
    # buffer_receive calls will block while <code>buffer_full? == true</code>.
    #
    # @return [bool] Is the buffer full?
    def buffer_full?
      @buffer_state[:pending_count] + @buffer_state[:outgoing_count] >= @buffer_config[:max_items]
    end

    # Save an event for later delivery
    #
    # Events are grouped by the (optional) group parameter you provide.
    # Groups of events, plus the group name, are later passed to +flush+.
    #
    # This call will block if +:max_items+ has been reached.
    #
    # @see Stud::Buffer The overview has more information on grouping and flushing.
    #
    # @param event An item to buffer for flushing later.
    # @param group Optional grouping key. All events with the same key will be
    #              passed to +flush+ together, along with the grouping key itself.
    def buffer_receive(event, group=nil)
      buffer_initialize if ! @buffer_state

      # block if we've accumulated too many events
      while buffer_full? do
        on_full_buffer_receive(
          :pending => @buffer_state[:pending_count],
          :outgoing => @buffer_state[:outgoing_count]
        ) if @buffer_config[:has_on_full_buffer_receive]
        sleep 0.1
      end

      @buffer_state[:pending_mutex].synchronize do
        @buffer_state[:pending_items][group] << event
        @buffer_state[:pending_count] += 1
      end

      buffer_flush
    end

    # Try to flush events.
    #
    # Returns immediately if flushing is not necessary/possible at the moment:
    # * :max_items have not been accumulated
    # * :max_interval seconds have not elapased since the last flush
    # * another flush is in progress
    #
    # <code>buffer_flush(:force => true)</code> will cause a flush to occur even
    # if +:max_items+ or +:max_interval+ have not been reached. A forced flush
    # will still return immediately (without flushing) if another flush is
    # currently in progress.
    #
    # <code>buffer_flush(:final => true)</code> is identical to <code>buffer_flush(:force => true)</code>,
    # except that if another flush is already in progress, <code>buffer_flush(:final => true)</code>
    # will block/wait for the other flush to finish before proceeding.
    #
    # @param [Hash] options Optional. May be <code>{:force => true}</code> or <code>{:final => true}</code>.
    # @return [Fixnum] The number of items successfully passed to +flush+.
    def buffer_flush(options={})
      force = options[:force] || options[:final]
      final = options[:final]

      # final flush will wait for lock, so we are sure to flush out all buffered events
      if options[:final]
        @buffer_state[:flush_mutex].lock
      elsif ! @buffer_state[:flush_mutex].try_lock # failed to get lock, another flush already in progress
        return 0
      end

      items_flushed = 0

      begin
        time_since_last_flush = (Time.now - @buffer_state[:last_flush])

        return 0 if @buffer_state[:pending_count] == 0
        return 0 if (!force) &&
           (@buffer_state[:pending_count] < @buffer_config[:max_items]) &&
           (time_since_last_flush < @buffer_config[:max_interval])

        @buffer_state[:pending_mutex].synchronize do
          @buffer_state[:outgoing_items] = @buffer_state[:pending_items]
          @buffer_state[:outgoing_count] = @buffer_state[:pending_count]
          buffer_clear_pending
        end

        @buffer_config[:logger].debug("Flushing output",
          :outgoing_count => @buffer_state[:outgoing_count],
          :time_since_last_flush => time_since_last_flush,
          :outgoing_events => @buffer_state[:outgoing_items],
          :batch_timeout => @buffer_config[:max_interval],
          :force => force,
          :final => final
        ) if @buffer_config[:logger]

        @buffer_state[:outgoing_items].each do |group, events|
          begin
            if group.nil?
              flush(events,final)
            else
              flush(events, group, final)
            end

            @buffer_state[:outgoing_items].delete(group)
            events_size = events.size
            @buffer_state[:outgoing_count] -= events_size
            items_flushed += events_size

          rescue => e

            @buffer_config[:logger].warn("Failed to flush outgoing items",
              :outgoing_count => @buffer_state[:outgoing_count],
              :exception => e.class.name,
              :backtrace => e.backtrace
            ) if @buffer_config[:logger]

            if @buffer_config[:has_on_flush_error]
              on_flush_error e
            end

            sleep 1
            retry
          end
          @buffer_state[:last_flush] = Time.now
        end

      ensure
        @buffer_state[:flush_mutex].unlock
      end

      return items_flushed
    end

    private
    def buffer_clear_pending
      @buffer_state[:pending_items] = Hash.new { |h, k| h[k] = [] }
      @buffer_state[:pending_count] = 0
    end
  end
end
