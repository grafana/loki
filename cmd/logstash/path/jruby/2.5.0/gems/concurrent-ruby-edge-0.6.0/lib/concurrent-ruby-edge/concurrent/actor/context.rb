require 'concurrent/concern/logging'

module Concurrent
  module Actor

    # New actor is defined by subclassing {RestartingContext}, {Context} and defining its abstract methods.
    # {AbstractContext} can be subclassed directly to implement more specific behaviour see {Root} implementation.
    #
    # -   {Context}
    #
    #     > {include:Actor::Context}
    #
    # -   {RestartingContext}.
    #
    #     > {include:Actor::RestartingContext}
    #
    # Example of ac actor definition:
    #
    # {include:file:docs-source/actor/define.out.rb}
    #
    # See methods of {AbstractContext} what else can be tweaked, e.g {AbstractContext#default_reference_class}
    #
    # @abstract implement {AbstractContext#on_message} and {AbstractContext#behaviour_definition}
    class AbstractContext
      include TypeCheck
      include InternalDelegations

      attr_reader :core

      # @abstract override to define Actor's behaviour
      # @param [Object] message
      # @return [Object] a result which will be used to set the Future supplied to Reference#ask
      # @note self should not be returned (or sent to other actors), {#reference} should be used
      #   instead
      def on_message(message)
        raise NotImplementedError
      end

      # override to add custom code invocation on internal events like `:terminated`, `:resumed`, `anError`.
      def on_event(event)
      end

      # @api private
      def on_envelope(envelope)
        @envelope = envelope
        on_message envelope.message
      ensure
        @envelope = nil
      end

      # if you want to pass the message to next behaviour, usually
      # {Behaviour::ErrorsOnUnknownMessage}
      def pass
        core.behaviour!(Behaviour::ExecutesContext).pass envelope
      end

      # Defines an actor responsible for dead letters. Any rejected message send
      # with {Reference#tell} is sent there, a message with future is considered
      # already monitored for failures. Default behaviour is to use
      # {AbstractContext#dead_letter_routing} of the parent, so if no
      # {AbstractContext#dead_letter_routing} method is overridden in
      # parent-chain the message ends up in `Actor.root.dead_letter_routing`
      # agent which will log warning.
      # @return [Reference]
      def dead_letter_routing
        parent.dead_letter_routing
      end

      # @return [Array<Array(Behavior::Abstract, Array<Object>)>]
      def behaviour_definition
        raise NotImplementedError
      end

      # @return [Envelope] current envelope, accessible inside #on_message processing
      def envelope
        @envelope or raise 'envelope not set'
      end

      # override if different class for reference is needed
      # @return [CLass] descendant of {Reference}
      def default_reference_class
        Reference
      end

      # override to se different default executor, e.g. to change it to global_operation_pool
      # @return [Executor]
      def default_executor
        Concurrent.global_io_executor
      end

      # tell self a message
      def tell(message)
        reference.tell message
      end

      def ask(message)
        raise 'actor cannot ask itself'
      end

      alias_method :<<, :tell
      alias_method :ask!, :ask

      # Behaves as {Concurrent::Actor.spawn} but :class is auto-inserted based on receiver so it can be omitted.
      # @example by class and name
      #   AdHoc.spawn(:ping1) { -> message { message } }
      #
      # @example by option hash
      #   inc2 = AdHoc.spawn(name:     'increment by 2',
      #                      args:     [2],
      #                      executor: Concurrent.configuration.global_task_pool) do |increment_by|
      #     lambda { |number| number + increment_by }
      #   end
      #   inc2.ask!(2) # => 4
      # @see Concurrent::Actor.spawn
      def self.spawn(name_or_opts, *args, &block)
        Actor.spawn to_spawn_options(name_or_opts, *args), &block
      end

      # behaves as {Concurrent::Actor.spawn!} but :class is auto-inserted based on receiver so it can be omitted.
      def self.spawn!(name_or_opts, *args, &block)
        Actor.spawn! to_spawn_options(name_or_opts, *args), &block
      end

      private

      def initialize_core(core)
        @core = Type! core, Core
      end

      def self.to_spawn_options(name_or_opts, *args)
        if name_or_opts.is_a? ::Hash
          if name_or_opts.key?(:class) && name_or_opts[:class] != self
            raise ArgumentError,
                  ':class option is ignored when calling on context class, use Actor.spawn instead'
          end
          name_or_opts.merge class: self
        else
          { class: self, name: name_or_opts, args: args }
        end
      end

      # to avoid confusion with Kernel.spawn
      undef_method :spawn
    end

    # Basic Context of an Actor. It supports only linking and it simply terminates on error.
    # Uses {Behaviour.basic_behaviour_definition}:
    #
    # @abstract implement {AbstractContext#on_message}
    class Context < AbstractContext
      def behaviour_definition
        Behaviour.basic_behaviour_definition
      end
    end

    # Context of an Actor for robust systems. It supports supervision, linking, pauses on error.
    # Uses {Behaviour.restarting_behaviour_definition}
    #
    # @abstract implement {AbstractContext#on_message}
    class RestartingContext < AbstractContext
      def behaviour_definition
        Behaviour.restarting_behaviour_definition
      end
    end
  end
end
