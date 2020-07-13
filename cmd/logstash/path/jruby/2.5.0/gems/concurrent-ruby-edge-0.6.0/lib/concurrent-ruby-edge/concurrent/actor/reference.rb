module Concurrent
  module Actor

    # {Reference} is public interface of Actor instances. It is used for sending messages and can
    # be freely passed around the application. It also provides some basic information about the actor,
    # see {PublicDelegations}.
    #
    #     AdHoc.spawn('printer') { -> message { puts message } }
    #     # => #<Concurrent::Actor::Reference:0x7fd0d2883218 /printer (Concurrent::Actor::Utils::AdHoc)>
    #     #                                   ^object_id     ^path     ^context class
    class Reference
      include TypeCheck
      include PublicDelegations

      attr_reader :core
      private :core

      # @!visibility private
      def initialize(core)
        @core = Type! core, Core
      end

      # Sends the message asynchronously to the actor and immediately returns
      # `self` (the reference) allowing to chain message telling.
      # @param [Object] message
      # @return [Reference] self
      # @example
      #   printer = AdHoc.spawn('printer') { -> message { puts message } }
      #   printer.tell('ping').tell('pong')
      #   printer << 'ping' << 'pong'
      #   # => 'ping'\n'pong'\n'ping'\n'pong'\n
      def tell(message)
        message message, nil
      end

      alias_method :<<, :tell

      # @note it's a good practice to use {#tell} whenever possible. Results can be sent back with other messages.
      #   Ask should be used only for testing and when it returns very shortly. It can lead to deadlock if all threads in
      #   global_io_executor will block on while asking. It's fine to use it form outside of actors and
      #   global_io_executor.
      # @param [Object] message
      # @param [Promises::Future] future to be fulfilled be message's processing result
      # @return [Promises::Future] supplied future
      # @example
      #   adder = AdHoc.spawn('adder') { -> message { message + 1 } }
      #   adder.ask(1).value # => 2
      #   adder.ask(nil).wait.reason # => #<NoMethodError: undefined method `+' for nil:NilClass>
      def ask(message, future = Concurrent::Promises.resolvable_future)
        message message, future
      end

      # @!visibility privated
      alias_method :ask_op, :ask

      # Sends the message synchronously and blocks until the message
      # is processed. Raises on error.
      #
      # @note it's a good practice to use {#tell} whenever possible. Results can be sent back with other messages.
      #   Ask should be used only for testing and when it returns very shortly. It can lead to deadlock if all threads in
      #   global_io_executor will block on while asking. It's fine to use it form outside of actors and
      #   global_io_executor.
      # @param [Object] message
      # @param [Promises::Future] future to be fulfilled be message's processing result
      # @return [Object] message's processing result
      # @raise [Exception] future.reason if future is #rejected?
      # @example
      #   adder = AdHoc.spawn('adder') { -> message { message + 1 } }
      #   adder.ask!(1) # => 2
      def ask!(message, future = Concurrent::Promises.resolvable_future)
        ask(message, future).value!
      end

      def map(messages)
        messages.map { |m| self.ask(m) }
      end

      # behaves as {#tell} when no future and as {#ask} when future
      def message(message, future = nil)
        core.on_envelope Envelope.new(message, future, Actor.current || Thread.current, self)
        return future ? future.with_hidden_resolvable : self
      end

      # @see AbstractContext#dead_letter_routing
      def dead_letter_routing
        core.dead_letter_routing
      end

      def to_s
        format '%s %s (%s)>', super[0..-2], path, actor_class
      end

      alias_method :inspect, :to_s

      def ==(other)
        Type? other, self.class and other.send(:core) == core
      end

      # to avoid confusion with Kernel.spawn
      undef_method :spawn
    end

  end
end
