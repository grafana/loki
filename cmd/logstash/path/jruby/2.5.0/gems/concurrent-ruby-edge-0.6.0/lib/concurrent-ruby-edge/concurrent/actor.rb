require 'concurrent/configuration'
require 'concurrent/executor/serialized_execution'
require 'concurrent/synchronization'
require 'concurrent/edge/promises'

module Concurrent
  # TODO https://github.com/celluloid/celluloid/wiki/Supervision-Groups ?
  # TODO Remote actors using DRb
  # TODO un/become
  # TODO supervision tree, pause children on error in parent, pause may need higher priority
  # TODO more effective executor

  # {include:file:docs-source/actor/main.md}
  # @api Actor
  # @!macro warn.edge
  module Actor

    require 'concurrent/actor/type_check'
    require 'concurrent/actor/errors'
    require 'concurrent/actor/public_delegations'
    require 'concurrent/actor/internal_delegations'
    require 'concurrent/actor/envelope'
    require 'concurrent/actor/reference'
    require 'concurrent/actor/core'
    require 'concurrent/actor/behaviour'
    require 'concurrent/actor/context'

    require 'concurrent/actor/default_dead_letter_handler'
    require 'concurrent/actor/root'
    require 'concurrent/actor/utils'

    # @return [Reference, nil] current executing actor if any
    def self.current
      Thread.current[:__current_actor__]
    end

    @root = Concurrent::Promises.delay do
      Core.
        new(parent:      nil,
            name:        '/',
            class:       Root,
            initialized: future = Concurrent::Promises.resolvable_future).
        reference.
        tap { future.wait! }
    end

    # A root actor, a default parent of all actors spawned outside an actor
    def self.root
      @root.value!
    end

    # Spawns a new actor. {Concurrent::Actor::AbstractContext.spawn} allows to omit class parameter.
    # To see the list of available options see {Core#initialize}
    # @see Concurrent::Actor::AbstractContext.spawn
    # @see Core#initialize
    # @example by class and name
    #   Actor.spawn(AdHoc, :ping1) { -> message { message } }
    #
    # @example by option hash
    #   inc2 = Actor.spawn(class:    AdHoc,
    #                      name:     'increment by 2',
    #                      args:     [2],
    #                      executor: Concurrent.global_io_executor) do |increment_by|
    #     lambda { |number| number + increment_by }
    #   end
    #   inc2.ask!(2) # => 4
    #
    # @param block for context_class instantiation
    # @param args see {.to_spawn_options}
    # @return [Reference] never the actual actor
    def self.spawn(*args, &block)
      options = to_spawn_options(*args)
      if options[:executor] && options[:executor].is_a?(ImmediateExecutor)
        raise ArgumentError, 'ImmediateExecutor is not supported'
      end
      if Actor.current
        Core.new(options.merge(parent: Actor.current), &block).reference
      else
        root.ask([:spawn, options, block]).value!
      end
    end

    # as {.spawn} but it'll block until actor is initialized or it'll raise exception on error
    def self.spawn!(*args, &block)
      spawn(to_spawn_options(*args).merge(initialized: future = Concurrent::Promises.resolvable_future), &block).tap { future.wait! }
    end

    # @overload to_spawn_options(context_class, name, *args)
    #   @param [AbstractContext] context_class to be spawned
    #   @param [String, Symbol] name of the instance, it's used to generate the
    #     {Core#path} of the actor
    #   @param args for context_class instantiation
    # @overload to_spawn_options(opts)
    #   see {Core#initialize} opts
    def self.to_spawn_options(*args)
      if args.size == 1 && args.first.is_a?(::Hash)
        args.first
      else
        { class: args[0],
          name:  args[1],
          args:  args[2..-1] }
      end
    end

  end
end
