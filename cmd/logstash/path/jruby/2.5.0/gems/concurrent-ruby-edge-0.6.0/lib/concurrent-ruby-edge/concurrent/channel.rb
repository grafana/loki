require 'forwardable'

require 'concurrent/channel/buffer'
require 'concurrent/channel/selector'

require 'concurrent/maybe'
require 'concurrent/executor/cached_thread_pool'

module Concurrent

  # {include:file:docs-source/channel.md}
  # @!macro warn.edge
  class Channel
    extend Forwardable
    include Enumerable

    # NOTE: Move to global IO pool once stable
    GOROUTINES = Concurrent::CachedThreadPool.new
    private_constant :GOROUTINES

    BUFFER_TYPES = {
      unbuffered: Buffer::Unbuffered,
      buffered: Buffer::Buffered,
      dropping: Buffer::Dropping,
      sliding: Buffer::Sliding
    }.freeze
    private_constant :BUFFER_TYPES

    DEFAULT_VALIDATOR = ->(value){ true }
    private_constant :DEFAULT_VALIDATOR

    Error = Class.new(StandardError)

    class ValidationError < Error
      def initialize(message = nil)
        message ||= 'invalid value'
      end
    end

    def_delegators :buffer,
      :size, :capacity, :close, :closed?,
      :blocking?, :empty?, :full?

    alias_method :length, :size
    alias_method :stop, :close

    def initialize(opts = {})
      # undocumented -- for internal use only
      if opts.is_a? Buffer::Base
        self.buffer = opts
        return
      end

      capacity = opts[:capacity] || opts[:size]
      buffer = opts[:buffer]

      if capacity && buffer == :unbuffered
        raise ArgumentError.new('unbuffered channels cannot have a capacity')
      elsif capacity.nil? && buffer.nil?
        self.buffer = BUFFER_TYPES[:unbuffered].new
      elsif capacity == 0 && buffer == :buffered
        self.buffer = BUFFER_TYPES[:unbuffered].new
      elsif buffer == :unbuffered
        self.buffer = BUFFER_TYPES[:unbuffered].new
      elsif capacity.nil? || capacity < 1
        raise ArgumentError.new('capacity must be at least 1 for this buffer type')
      else
        buffer ||= :buffered
        self.buffer = BUFFER_TYPES[buffer].new(capacity)
      end

      self.validator = opts.fetch(:validator, DEFAULT_VALIDATOR)
    end

    def put(item)
      return false unless validate(item, false, false)
      do_put(item)
    end
    alias_method :send, :put
    alias_method :<<, :put

    def put!(item)
      validate(item, false, true)
      ok = do_put(item)
      raise Error if !ok
      ok
    end

    def put?(item)
      if !validate(item, true, false)
        Concurrent::Maybe.nothing('invalid value')
      elsif do_put(item)
        Concurrent::Maybe.just(true)
      else
        Concurrent::Maybe.nothing
      end
    end

    def offer(item)
      return false unless validate(item, false, false)
      do_offer(item)
    end

    def offer!(item)
      validate(item, false, true)
      ok = do_offer(item)
      raise Error if !ok
      ok
    end

    def offer?(item)
      if !validate(item, true, false)
        Concurrent::Maybe.nothing('invalid value')
      elsif do_offer(item)
        Concurrent::Maybe.just(true)
      else
        Concurrent::Maybe.nothing
      end
    end

    def take
      item = do_take
      item == Concurrent::NULL ? nil : item
    end
    alias_method :receive, :take
    alias_method :~, :take

    def take!
      item = do_take
      raise Error if item == Concurrent::NULL
      item
    end

    def take?
      item = do_take
      item = if item == Concurrent::NULL
               Concurrent::Maybe.nothing
             else
               Concurrent::Maybe.just(item)
             end
      item
    end

    # @example
    #
    #   jobs = Channel.new
    #
    #   Channel.go do
    #     loop do
    #       j, more = jobs.next
    #       if more
    #         print "received job #{j}\n"
    #       else
    #         print "received all jobs\n"
    #         break
    #       end
    #     end
    #   end
    def next
      item, more = do_next
      item = nil if item == Concurrent::NULL
      return item, more
    end

    def next?
      item, more = do_next
      item = if item == Concurrent::NULL
               Concurrent::Maybe.nothing
             else
               Concurrent::Maybe.just(item)
             end
      return item, more
    end

    def poll
      (item = do_poll) == Concurrent::NULL ? nil : item
    end

    def poll!
      item = do_poll
      raise Error if item == Concurrent::NULL
      item
    end

    def poll?
      if (item = do_poll) == Concurrent::NULL
        Concurrent::Maybe.nothing
      else
        Concurrent::Maybe.just(item)
      end
    end

    def each
      raise ArgumentError.new('no block given') unless block_given?
      loop do
        item, more = do_next
        if item != Concurrent::NULL
          yield(item)
        elsif !more
          break
        end
      end
    end

    class << self
      def timer(seconds)
        Channel.new(Buffer::Timer.new(seconds))
      end
      alias_method :after, :timer

      def ticker(interval)
        Channel.new(Buffer::Ticker.new(interval))
      end
      alias_method :tick, :ticker

      def select(*args)
        raise ArgumentError.new('no block given') unless block_given?
        selector = Selector.new
        yield(selector, *args)
        selector.execute
      end
      alias_method :alt, :select

      def go(*args, &block)
        go_via(GOROUTINES, *args, &block)
      end

      def go_via(executor, *args, &block)
        raise ArgumentError.new('no block given') unless block_given?
        executor.post(*args, &block)
      end

      def go_loop(*args, &block)
        go_loop_via(GOROUTINES, *args, &block)
      end

      def go_loop_via(executor, *args, &block)
        raise ArgumentError.new('no block given') unless block_given?
        executor.post(block, *args) do
          loop do
            break unless block.call(*args)
          end
        end
      end
    end

    private

    def validator
      @validator
    end

    def validator=(value)
      @validator = value
    end

    def buffer
      @buffer
    end

    def buffer=(value)
      @buffer = value
    end

    def validate(value, allow_nil, raise_error)
      if !allow_nil && value.nil?
        raise_error ? raise(ValidationError.new('nil is not a valid value')) : false
      elsif !validator.call(value)
        raise_error ? raise(ValidationError) : false
      else
        true
      end
    rescue => ex
      # the validator raised an exception
      return raise_error ? raise(ex) : false
    end

    def do_put(item)
      buffer.put(item)
    end

    def do_offer(item)
      buffer.offer(item)
    end

    def do_take
      buffer.take
    end

    def do_next
      buffer.next
    end

    def do_poll
      buffer.poll
    end
  end
end
