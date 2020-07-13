require 'concurrent/concern/logging'
require 'concurrent/executors'

module Concurrent
  module Actor

    require 'set'

    # Core of the actor.
    # @note Whole class should be considered private. An user should use {Context}s and {Reference}s only.
    # @note devel: core should not block on anything, e.g. it cannot wait on children to terminate
    #   that would eat up all threads in task pool and deadlock
    class Core < Synchronization::LockableObject
      include TypeCheck
      include Concern::Logging

      # @!attribute [r] reference
      #   Reference to this actor which can be safely passed around.
      #   @return [Reference]
      # @!attribute [r] name
      #   The name of actor instance, it should be uniq (not enforced). Allows easier orientation
      #   between actor instances.
      #   @return [String]
      # @!attribute [r] path
      #   Path of this actor. It is used for easier orientation and logging.
      #   Path is constructed recursively with: `parent.path + self.name` up to a {Actor.root},
      #   e.g. `/an_actor/its_child`.
      #   @return [String]
      # @!attribute [r] executor
      #   Executor which is used to process messages.
      #   @return [Executor]
      # @!attribute [r] actor_class
      #   A subclass of {AbstractContext} representing Actor's behaviour.
      #   @return [Context]
      attr_reader :reference, :name, :path, :executor, :context_class, :context, :behaviour_definition

      # @option opts [String] name
      # @option opts [Context] actor_class a class to be instantiated defining Actor's behaviour
      # @option opts [Array<Object>] args arguments for actor_class instantiation
      # @option opts [Executor] executor, default is `global_io_executor`
      # @option opts [true, false] link, atomically link the actor to its parent (default: true)
      # @option opts [Class] reference a custom descendant of {Reference} to use
      # @option opts [Array<Array(Behavior::Abstract, Array<Object>)>] behaviour_definition, array of pairs
      #   where each pair is behaviour class and its args, see {Behaviour.basic_behaviour_definition}
      # @option opts [ResolvableFuture, nil] initialized, if present it'll be set or failed after {Context} initialization
      # @option opts [Reference, nil] parent **private api** parent of the actor (the one spawning )
      # @option opts [Proc, nil] logger a proc accepting (level, progname, message = nil, &block) params,
      #   can be used to hook actor instance to any logging system, see {Concurrent::Concern::Logging}
      # @param [Proc] block for class instantiation
      def initialize(opts = {}, &block)
        super(&nil)
        synchronize { ns_initialize(opts, &block) }
      end

      # A parent Actor. When actor is spawned the {Actor.current} becomes its parent.
      # When actor is spawned from a thread outside of an actor ({Actor.current} is nil) {Actor.root} is assigned.
      # @return [Reference, nil]
      def parent
        @parent_core && @parent_core.reference
      end

      # @see AbstractContext#dead_letter_routing
      def dead_letter_routing
        @context.dead_letter_routing
      end

      # @return [Array<Reference>] of children actors
      def children
        guard!
        @children.to_a
      end

      # @api private
      def add_child(child)
        guard!
        Type! child, Reference
        @children.add child
        nil
      end

      # @api private
      def remove_child(child)
        guard!
        Type! child, Reference
        @children.delete child
        nil
      end

      # is executed by Reference scheduling processing of new messages
      # can be called from other alternative Reference implementations
      # @param [Envelope] envelope
      def on_envelope(envelope)
        log(DEBUG) { "is  #{envelope.future ? 'asked' : 'told'} #{envelope.message.inspect} by #{envelope.sender}" }
        schedule_execution do
          log(DEBUG) { "was #{envelope.future ? 'asked' : 'told'} #{envelope.message.inspect} by #{envelope.sender} - processing" }
          process_envelope envelope
        end
        nil
      end

      # ensures that we are inside of the executor
      def guard!
        unless Actor.current == reference
          raise "can be called only inside actor #{reference} but was #{Actor.current}"
        end
      end

      def log(level, message = nil, &block)
        super level, @path, message, &block
      end

      # Schedules blocks to be executed on executor sequentially,
      # sets Actress.current
      def schedule_execution
        @serialized_execution.post(@executor) do
          synchronize do
            begin
              Thread.current[:__current_actor__] = reference
              yield
            rescue => e
              log FATAL, e
            ensure
              Thread.current[:__current_actor__] = nil
            end
          end
        end

        nil
      end

      def broadcast(public, event)
        log(DEBUG) { "event: #{event.inspect} (#{public ? 'public' : 'private'})" }
        @first_behaviour.on_event(public, event)
      end

      # @param [Class] behaviour_class
      # @return [Behaviour::Abstract, nil] based on behaviour_class
      def behaviour(behaviour_class)
        @behaviours[behaviour_class]
      end

      # @param [Class] behaviour_class
      # @return [Behaviour::Abstract] based on behaviour_class
      # @raise [KeyError] when no behaviour
      def behaviour!(behaviour_class)
        @behaviours.fetch behaviour_class
      end

      # @api private
      def allocate_context
        @context = @context_class.allocate
      end

      # @api private
      def build_context
        @context.send :initialize_core, self
        @context.send :initialize, *@args, &@block
      end

      # @api private
      def process_envelope(envelope)
        @first_behaviour.on_envelope envelope
      end

      private

      def ns_initialize(opts, &block)
        @mailbox              = ::Array.new
        @serialized_execution = SerializedExecution.new
        @children             = Set.new

        @context_class = Child! opts.fetch(:class), AbstractContext
        allocate_context

        @executor = Type! opts.fetch(:executor, @context.default_executor), Concurrent::AbstractExecutorService

        @reference = (Child! opts[:reference_class] || @context.default_reference_class, Reference).new self
        @name      = (Type! opts.fetch(:name), String, Symbol).to_s

        parent       = opts[:parent]
        @parent_core = (Type! parent, Reference, NilClass) && parent.send(:core)
        if @parent_core.nil? && @name != '/'
          raise 'only root has no parent'
        end

        @path   = @parent_core ? File.join(@parent_core.path, @name) : @name
        @logger = opts[:logger]

        @parent_core.add_child reference if @parent_core

        initialize_behaviours opts

        @args       = opts.fetch(:args, [])
        @block      = block
        initialized = Type! opts[:initialized], Promises::ResolvableFuture, NilClass

        schedule_execution do
          begin
            build_context
            initialized.fulfill reference if initialized
            log DEBUG, 'spawned'
          rescue => ex
            log ERROR, ex
            @first_behaviour.terminate!
            initialized.reject ex if initialized
          end
        end
      end

      def initialize_behaviours(opts)
        @behaviour_definition = (Type! opts[:behaviour_definition] || @context.behaviour_definition, ::Array).each do |(behaviour, _)|
          Child! behaviour, Behaviour::Abstract
        end
        @behaviours           = {}
        @first_behaviour      = @behaviour_definition.reverse.
            reduce(nil) { |last, (behaviour, *args)| @behaviours[behaviour] = behaviour.new(self, last, opts, *args) }
      end
    end
  end
end
