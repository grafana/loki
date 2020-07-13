if Concurrent.ruby_version :<, 2, 1, 0
  raise 'ErlangActor requires at least ruby version 2.1'
end

module Concurrent

  # This module provides actor abstraction that has same behaviour as Erlang actor.
  #
  # {include:file:docs-source/erlang_actor.out.md}
  # @!macro warn.edge
  module ErlangActor

    # TODO (pitr-ch 04-Feb-2019): mode documentation.
    # TODO (pitr-ch 21-Jan-2019): actor on promises should not call blocking calls like mailbox.pop or tell
    # it's fine for a actor on thread and event based though
    # TODO (pitr-ch 17-Jan-2019): blocking actor should react to signals?
    # e.g. override sleep to wait for signal with a given timeout?
    # what about other blocking stuff
    # def sleep(time)
    #   raise NotImplementedError
    # end
    #
    # def sleep(time)
    #   raise NotImplementedError
    #   finish = Concurrent.monotonic_time + time
    #   while true
    #     now = Concurrent.monotonic_time
    #     if now < finish
    #       message = @Mailbox.pop_matching(AbstractSignal, finish - now)
    #     else
    #     end
    #   end
    # end
    # TODO (pitr-ch 28-Jan-2019): improve matching support, take inspiration and/or port Algebrick ideas, push ANY and similar further up the namespace

    # The public reference of the actor which can be stored and passed around.
    # Nothing else of the actor should be exposed.
    # {Functions.spawn_actor} and {Environment#spawn} return the pid.
    class Pid < Synchronization::Object
      # TODO (pitr-ch 06-Feb-2019): when actor terminates, release it from memory keeping just pid

      # The actor is asynchronously told a message.
      # The method returns immediately unless
      # the actor has bounded mailbox and there is no more space for the message.
      # Then the method blocks current thread until there is space available.
      # This is useful for backpressure.
      #
      # @param [Object] message
      # @param [Numeric] timeout the maximum time in second to wait
      # @return [self, true, false]
      #   self if timeout was nil, false on timing out and true if told in time.
      def tell(message, timeout = nil)
        @Actor.tell message, timeout
      end

      # Same as {#tell} but represented as a {Promises::Future}.
      # @param [Object] message
      # @return [Promises::Future(self)]
      def tell_op(message)
        @Actor.tell_op(message)
      end

      # The actor is asked the message and blocks until a reply is available,
      # which is returned by the method.
      # If the reply is a rejection then the methods raises it.
      #
      # If the actor does not call {Environment#reply} or {Environment#reply_resolution}
      # the method will raise NoReply error.
      # If the actor is terminated it will raise NoActor.
      # Therefore the ask is never left unanswered and blocking.
      #
      # @param [Object] message
      # @param [Numeric] timeout the maximum time in second to wait
      # @param [Object] timeout_value the value returned on timeout
      # @return [Object, timeout_value] reply to the message
      # @raise [NoReply, NoActor]
      def ask(message, timeout = nil, timeout_value = nil)
        @Actor.ask message, timeout, timeout_value
      end

      # Same as {#tell} but represented as a {Promises::Future}.
      # @param [Object] message
      # @param [Promises::ResolvableFuture] probe
      #   a resolvable future which is resolved with the reply.
      # @return [Promises::Future(Object)] reply to the message
      def ask_op(message, probe = Promises.resolvable_future)
        @Actor.ask_op message, probe
      end

      # @!macro erlang_actor.terminated
      #   @return [Promises::Future] a future which is resolved with
      #     the final result of the actor that is either the reason for
      #     termination or a value if terminated normally.
      def terminated
        @Actor.terminated
      end

      # @return [#to_s, nil] optional name of the actor
      def name
        @Name
      end

      # @return [String] string representation
      def to_s
        original = super
        state    = case terminated.state
                   when :pending
                     'running'
                   when :fulfilled
                     "terminated normally with #{terminated.value}"
                   when :rejected
                     "terminated because of #{terminated.reason}"
                   else
                     raise
                   end
        [original[0..-2], *@Name, state].join(' ') << '>'
      end

      alias_method :inspect, :to_s

      private

      safe_initialization!

      def initialize(actor, name)
        @Actor = actor
        @Name  = name
      end
    end

    # An object representing instance of a monitor, created with {Environment#monitor}.
    class Reference
    end

    # A class providing environment and methods for actor bodies to run in.
    class Environment < Synchronization::Object
      safe_initialization!

      # @!macro erlang_actor.terminated
      def terminated
        @Actor.terminated
      end

      # @return [Pid] the pid of this actor
      def pid
        @Actor.pid
      end

      # @return [#to_s] the name od the actor if provided to spawn method
      def name
        pid.name
      end

      # @return [true, false] does this actor trap exit messages?
      # @see http://www1.erlang.org/doc/man/erlang.html#process_flag-2
      def traps?
        @Actor.traps?
      end

      # When trap is set to true,
      # exit signals arriving to a actor are converted to {Terminated} messages,
      # which can be received as ordinary messages.
      # If trap is set to false,
      # the actor exits
      # if it receives an exit signal other than normal
      # and the exit signal is propagated to its linked actors.
      # Application actors should normally not trap exits.
      #
      # @param [true, false] value
      # @return [true, false] the old value of the flag
      # @see http://www1.erlang.org/doc/man/erlang.html#process_flag-2
      def trap(value = true)
        @Actor.trap(value)
      end

      # Helper for constructing a {#receive} rules
      # @see #receive
      # @example
      #   receive on(Numeric) { |v| v.succ },
      #           on(ANY) { terminate :bad_message }
      def on(matcher, value = nil, &block)
        @Actor.on matcher, value, &block
      end

      # Receive a message.
      #
      # @param [::Array(), ::Array(#===), ::Array<::Array(#===, Proc)>] rules
      #   *   No rule - `receive`, `receive {|m| m.to_s}`
      #   *   or single rule which can be combined with the supplied block -
      #       `receive(Numeric)`, `receive(Numeric) {|v| v.succ}`
      #   *   or array of matcher-proc pairs -
      #       `receive on(Numeric) { |v| v*2 }, on(Symbol) { |c| do_command c }`
      # @param [Numeric] timeout
      #   how long it should wait for the message
      # @param [Object] timeout_value
      #   if rule `on(TIMEOUT) { do_something }` is not specified
      #   then timeout_value is returned.
      # @return [Object, nothing]
      #   depends on type of the actor.
      #   On thread it blocks until message is available
      #   then it returns the message (or a result of a called block).
      #   On pool it stops executing and continues with a given block
      #   when message becomes available.
      # @param [Hash] options
      #   other options specific by type of the actor
      # @option options [true, false] :keep
      #   Keep the rules and repeatedly call the associated blocks,
      #   until receive is called again.
      # @yield [message] block
      #   to process the message
      #   if single matcher is supplied
      # @yieldparam [Object] message the received message
      # @see ErlangActor Receiving chapter in the ErlangActor examples
      def receive(*rules, timeout: nil, timeout_value: nil, **options, &block)
        @Actor.receive(*rules, timeout: timeout, timeout_value: timeout_value, **options, &block)
      end

      # Creates a link between the calling actor and another actor,
      # if there is not such a link already.
      # If a actor attempts to create a link to itself, nothing is done. Returns true.
      #
      # If pid does not exist,
      # the behavior of the method depends on
      # if the calling actor is trapping exits or not (see {#trap}):
      # * If the calling actor is not trapping exits link raises with {NoActor}.
      # * Otherwise, if the calling actor is trapping exits, link returns true,
      #   but an exit signal with reason noproc is sent to the calling actor.
      #
      # @return [true]
      # @raise [NoActor]
      # @see http://www1.erlang.org/doc/man/erlang.html#link-1
      def link(pid)
        @Actor.link(pid)
      end

      # Removes the link, if there is one,
      # between the calling actor and the actor referred to by pid.
      #
      # Returns true and does not fail, even if there is no link to Id, or if Id does not exist.
      #
      # Once unlink(pid) has returned
      # it is guaranteed
      # that the link between the caller and the actor referred to by pid
      # has no effect on the caller in the future (unless the link is setup again).
      # If caller is trapping exits,
      # an {Terminated} message due to the link might have been placed
      # in the caller's message queue prior to the call, though.
      #
      # Note, the {Terminated} message can be the result of the link,
      # but can also be the result of calling #terminate method externally.
      # Therefore, it may be appropriate to cleanup the message queue
      # when trapping exits after the call to unlink, as follow:
      # ```ruby
      # receive on(And[Terminated, -> e { e.pid == pid }], true), timeout: 0
      # ```
      #
      # @return [true]
      def unlink(pid)
        @Actor.unlink(pid)
      end

      # @!visibility private
      # @return [true, false]
      def linked?(pid)
        @Actor.linked? pid
      end

      # The calling actor starts monitoring actor with given pid.
      #
      # A {DownSignal} message will be sent to the monitoring actor
      # if the actor with given pid dies,
      # or if the actor with given pid does not exist.
      #
      # The monitoring is turned off either
      # when the {DownSignal} message is sent, or when {#demonitor} is called.
      #
      # Making several calls to monitor for the same pid is not an error;
      # it results in as many, completely independent, monitorings.
      #
      # @return [Reference]
      def monitor(pid)
        @Actor.monitor(pid)
      end

      # If MonitorRef is a reference which the calling actor obtained by calling {#monitor},
      # this monitoring is turned off.
      # If the monitoring is already turned off, nothing happens.
      #
      # Once demonitor has returned it is guaranteed that no {DownSignal} message
      # due to the monitor will be placed in the caller's message queue in the future.
      # A {DownSignal} message might have been placed in the caller's message queue prior to the call, though.
      # Therefore, in most cases, it is advisable to remove such a 'DOWN' message from the message queue
      # after monitoring has been stopped.
      # `demonitor(reference, :flush)` can be used if this cleanup is wanted.
      #
      # The behavior of this method can be viewed as two combined operations:
      # asynchronously send a "demonitor signal" to the monitored actor and
      # ignore any future results of the monitor.
      #
      # Failure: It is an error if reference refers to a monitoring started by another actor.
      # In that case it may raise an ArgumentError or go unnoticed.
      #
      # Options:
      # *   `:flush` - Remove (one) {DownSignal} message,
      #     if there is one, from the caller's message queue after monitoring has been stopped.
      #     Calling `demonitor(pid, :flush)` is equivalent to the following, but more efficient:
      #     ```ruby
      #     demonitor(pid)
      #     receive on(And[DownSignal, -> d { d.reference == reference}], true), timeout: 0, timeout_value: true
      #     ```
      #
      # *   `info`
      #     The returned value is one of the following:
      #
      #     -   `true` - The monitor was found and removed.
      #         In this case no {DownSignal} message due to this monitor have been
      #         nor will be placed in the message queue of the caller.
      #     -   `false` - The monitor was not found and could not be removed.
      #         This probably because someone already has placed a {DownSignal} message
      #         corresponding to this monitor in the caller's message queue.
      #
      #     If the info option is combined with the flush option,
      #     `false` will be returned if a flush was needed; otherwise, `true`.
      #
      # @param [Reference] reference
      # @param [:flush, :info] options
      # @return [true, false]
      def demonitor(reference, *options)
        @Actor.demonitor(reference, *options)
      end

      # @!visibility private
      def monitoring?(reference)
        @Actor.monitoring? reference
      end

      # Creates an actor.
      #
      # @param [Object] args arguments for the actor body
      # @param [:on_thread, :on_pool] type
      #   of the actor to be created.
      # @param [Channel] channel
      #   The mailbox of the actor, by default it has unlimited capacity.
      #   Crating the actor with a bounded queue is useful to create backpressure.
      #   The channel can be shared with other abstractions
      #   but actor has to be the only consumer
      #   otherwise internal signals could be lost.
      # @param [Environment, Module] environment
      #   A class which is used to run the body of the actor in.
      #   It can either be a child of {Environment} or a module.
      #   Module is extended to a new instance of environment,
      #   therefore if there is many actors with this module
      #   it is better to create a class and use it instead.
      # @param [#to_s] name of the actor.
      #   Available by {Pid#name} or {Environment#name} and part of {Pid#to_s}.
      # @param [true, false] link
      #   the created actor is atomically created and linked with the calling actor
      # @param [true, false] monitor
      #   the created actor is atomically created and monitored by the calling actor
      # @param [ExecutorService] executor
      #   The executor service to use to execute the actor on.
      #   Applies only to :on_pool actor type.
      # @yield [*args] the body of the actor.
      #   When actor is spawned this block is evaluated
      #   until it terminates.
      #   The on-thread actor requires a block.
      #   The on-poll actor has a default `-> { start }`,
      #   therefore if not block is given it executes a #start method
      #   which needs to be provided with environment.
      # @return [Pid, ::Array(Pid, Reference)] a pid or a pid-reference pair when monitor is true
      # @see http://www1.erlang.org/doc/man/erlang.html#spawn-1
      # @see http://www1.erlang.org/doc/man/erlang.html#spawn_link-1
      # @see http://www1.erlang.org/doc/man/erlang.html#spawn_monitor-1
      def spawn(*args,
                type: @Actor.class,
                channel: Promises::Channel.new,
                environment: Environment,
                name: nil,
                executor: default_executor,
                link: false,
                monitor: false,
                &body)

        @Actor.spawn(*args,
                     type:        type,
                     channel:     channel,
                     environment: environment,
                     name:        name,
                     executor:    executor,
                     link:        link,
                     monitor:     monitor,
                     &body)
      end

      # Shortcut for fulfilling the reply, same as `reply_resolution true, value, nil`.
      # @example
      #   actor = Concurrent::ErlangActor.spawn(:on_thread) { reply receive * 2 }
      #   actor.ask 2 #=> 4
      # @param [Object] value
      # @return [true, false] did the sender ask, and was it resolved
      def reply(value)
        # TODO (pitr-ch 08-Feb-2019): consider adding reply? which returns true,false if success, reply method will always return value
        reply_resolution true, value, nil
      end

      # Reply to the sender of the message currently being processed
      # if the actor was asked instead of told.
      # The reply is stored in a {Promises::ResolvableFuture}
      # so the arguments are same as for {Promises::ResolvableFuture#resolve} method.
      #
      # The reply may timeout, then this will fail with false.
      #
      # @param [true, false] fulfilled
      # @param [Object] value
      # @param [Object] reason
      #
      # @example
      #   actor = Concurrent::ErlangActor.spawn(:on_thread) { reply_resolution true, receive * 2, nil }
      #   actor.ask 2 #=> 4
      #
      # @return [true, false] did the sender ask, and was it resolved before it timed out?
      def reply_resolution(fulfilled = true, value = nil, reason = nil)
        @Actor.reply_resolution(fulfilled, value, reason)
      end

      # If pid **is not** provided stops the execution of the calling actor
      # with the exit reason.
      #
      # If pid **is** provided,
      # it sends an exit signal with exit reason to the actor identified by pid.
      #
      # The following behavior apply
      # if `reason` is any object except `:normal` or `:kill`.
      # If pid is not trapping exits,
      # pid itself will exit with exit reason.
      # If pid is trapping exits,
      # the exit signal is transformed into a message {Terminated}
      # and delivered to the message queue of pid.
      #
      # If reason is the Symbol `:normal`, pid will not exit.
      # If it is trapping exits, the exit signal is transformed into a message {Terminated}
      # and delivered to its message queue.
      #
      # If reason is the Symbol `:kill`, that is if `exit(pid, :kill)` is called,
      # an untrappable exit signal is sent to pid which will unconditionally exit
      # with exit reason `:killed`.
      #
      # Since evaluating this function causes the process to terminate, it has no return value.
      #
      # @param [Pid] pid
      # @param [Object, :normal, :kill] reason
      # @param [Object] value
      # @return [nothing]
      # @see http://www1.erlang.org/doc/man/erlang.html#error-1
      # @see http://www1.erlang.org/doc/man/erlang.html#error-2
      def terminate(pid = nil, reason, value: nil)
        @Actor.terminate pid, reason, value: value
      end

      # @return [ExecutorService] a default executor which is picked by spawn call
      def default_executor
        @DefaultExecutor
      end

      private

      def initialize(actor, executor)
        super()
        @Actor           = actor
        @DefaultExecutor = executor
      end
    end

    # A module containing entry functions to actors like spawn_actor, terminate_actor.
    # It can be included in environments working with actors.
    # @example
    #   include Concurrent::ErlangActors::Functions
    #   actor = spawn_actor :on_pool do
    #     receive { |data| process data }
    #   end
    # @see FunctionShortcuts
    module Functions
      # Creates an actor. Same as {Environment#spawn} but lacks link and monitor options.
      # @param [Object] args
      # @param [:on_thread, :on_pool] type
      # @param [Channel] channel
      # @param [Environment, Module] environment
      # @param [#to_s] name of the actor
      # @param [ExecutorService] executor of the actor
      # @return [Pid]
      # @see Environment#spawn
      def spawn_actor(*args,
                      type:,
                      channel: Promises::Channel.new,
                      environment: Environment,
                      name: nil,
                      executor: default_actor_executor,
                      &body)

        actor = ErlangActor.create type, channel, environment, name, executor
        actor.run(*args, &body)
        return actor.pid
      end

      # Same as {Environment#terminate}, but it requires pid.
      # @param [Pid] pid
      # @param [Object, :normal, :kill] reason
      # @return [true]
      def terminate_actor(pid, reason)
        if reason == :kill
          pid.tell Kill.new(nil)
        else
          pid.tell Terminate.new(nil, reason, false)
        end
        true
      end

      # @return [ExecutorService] the default executor service for actors
      def default_actor_executor
        default_executor
      end

      # @return [ExecutorService] the default executor service,
      #   may be shared by other abstractions
      def default_executor
        :io
      end
    end

    # Constrains shortcuts for methods in {Functions}.
    module FunctionShortcuts
      # Optionally included shortcut method for {Functions#spawn_actor}
      # @return [Pid]
      def spawn(*args, **kwargs, &body)
        spawn_actor(*args, **kwargs, &body)
      end

      # Optionally included shortcut method for {Functions#terminate_actor}
      # @return [true]
      def terminate(pid, reason)
        terminate_actor(pid, reason)
      end
    end

    extend Functions
    extend FunctionShortcuts
    extend Concern::Logging

    class Token
      def initialize(name)
        @name = name
      end

      def to_s
        @name
      end

      alias_method :inspect, :to_s
    end

    private_constant :Token

    JUMP      = Token.new 'JUMP'
    TERMINATE = Token.new 'TERMINATE'
    RECEIVE   = Token.new 'RECEIVE'
    NOTHING   = Token.new 'NOTHING'

    private_constant :JUMP
    private_constant :TERMINATE
    private_constant :RECEIVE
    private_constant :NOTHING

    # These constants are useful
    # where the body of an actor is defined.
    # For convenience they are provided in this module for including.
    # @example
    #   include Concurrent::ErlangActor::EnvironmentConstants
    #   actor = Concurrent::ErlangActor.spawn(:on_thread) do
    #     receive on(Numeric) { |v| v.succ },
    #             on(ANY) { terminate :bad_message },
    #             on(TIMEOUT) { terminate :no_message },
    #             timeout: 1
    #   end
    module EnvironmentConstants
      # Unique identifier of a timeout, singleton.
      TIMEOUT = Token.new 'TIMEOUT'
      # A singleton which matches anything using #=== method
      ANY = Promises::Channel::ANY

      class AbstractLogicOperationMatcher
        def self.[](*matchers)
          new(*matchers)
        end

        def initialize(*matchers)
          @matchers = matchers
        end
      end

      # Combines matchers into one which matches if all match.
      # @example
      #   And[Numeric, -> v { v >= 0 }] === 1  # => true
      #   And[Numeric, -> v { v >= 0 }] === -1 # => false
      class And < AbstractLogicOperationMatcher
        # @return [true, false]
        def ===(v)
          @matchers.all? { |m| m === v }
        end
      end

      # Combines matchers into one which matches if any matches.
      # @example
      #   Or[Symbol, String] === :v  # => true
      #   Or[Symbol, String] === 'v' # => true
      #   Or[Symbol, String] === 1   # => false
      class Or < AbstractLogicOperationMatcher
        # @return [true, false]
        def ===(v)
          @matchers.any? { |m| m === v }
        end
      end
    end

    include EnvironmentConstants

    class Run
      attr_reader :future

      def self.[](future)
        new future
      end

      def initialize(future)
        @future = future
      end

      TEST = -> v { v.future if v.is_a?(Run) }
    end
    private_constant :Run

    class AbstractActor < Synchronization::Object

      include EnvironmentConstants
      include Concern::Logging
      safe_initialization!

      # @param [Promises::Channel] mailbox
      def initialize(mailbox, environment, name, executor)
        super()
        @Mailbox                = mailbox
        @Pid                    = Pid.new self, name
        @Linked                 = ::Set.new
        @Monitors               = {}
        @Monitoring             = {}
        @MonitoringLateDelivery = {}
        @Terminated             = Promises.resolvable_future
        @trap                   = false
        @reply                  = nil

        @Environment = if environment.is_a?(Class) && environment <= Environment
                         environment.new self, executor
                       elsif environment.is_a? Module
                         e = Environment.new self, executor
                         e.extend environment
                         e
                       else
                         raise ArgumentError,
                               "environment has to be a class inheriting from Environment or a module"
                       end
      end

      def tell_op(message)
        log Logger::DEBUG, @Pid, told: message
        if (mailbox = @Mailbox)
          mailbox.push_op(message).then { @Pid }
        else
          Promises.fulfilled_future @Pid
        end
      end

      def tell(message, timeout = nil)
        log Logger::DEBUG, @Pid, told: message
        if (mailbox = @Mailbox)
          timed_out = mailbox.push message, timeout
          timeout ? timed_out : @Pid
        else
          timeout ? false : @Pid
        end
      end

      def ask(message, timeout, timeout_value)
        log Logger::DEBUG, @Pid, asked: message
        if @Terminated.resolved?
          raise NoActor.new(@Pid)
        else
          probe    = Promises.resolvable_future
          question = Ask.new(message, probe)
          if timeout
            start   = Concurrent.monotonic_time
            in_time = tell question, timeout
            # recheck it could have in the meantime terminated and drained mailbox
            raise NoActor.new(@Pid) if @Terminated.resolved?
            # has to be after resolved check, to catch case where it would return timeout_value
            # when it was actually terminated
            to_wait = if in_time
                        time = timeout - (Concurrent.monotonic_time - start)
                        time >= 0 ? time : 0
                      else
                        0
                      end
            # TODO (pitr-ch 06-Feb-2019): allow negative timeout everywhere, interpret as 0
            probe.value! to_wait, timeout_value, [true, nil, nil]
          else
            raise NoActor.new(@Pid) if @Terminated.resolved?
            tell question
            probe.reject NoActor.new(@Pid), false if @Terminated.resolved?
            probe.value!
          end
        end
      end

      def ask_op(message, probe)
        log Logger::DEBUG, @Pid, asked: message
        if @Terminated.resolved?
          probe.reject NoActor.new(@Pid), false
        else
          tell_op(Ask.new(message, probe)).then do
            probe.reject NoActor.new(@Pid), false if @Terminated.resolved?
            probe
          end.flat
        end
      end

      def terminated
        @Terminated.with_hidden_resolvable
      end

      def pid
        @Pid
      end

      def traps?
        @trap
      end

      def trap(value = true)
        old = @trap
        # noinspection RubySimplifyBooleanInspection
        @trap = !!value
        old
      end

      def on(matcher, value = nil, &block)
        raise ArgumentError, 'only one of block or value can be supplied' if block && value
        [matcher, value || block]
      end

      def receive(*rules, timeout: nil, timeout_value: nil, **options, &block)
        raise NotImplementedError
      end

      def link(pid)
        return true if pid == @Pid
        if @Linked.add? pid
          pid.tell Link.new(@Pid)
          if pid.terminated.resolved?
            # no race since it only could get NoActor
            if @trap
              tell Terminate.new pid, NoActor.new(pid)
            else
              @Linked.delete pid
              raise NoActor.new(pid)
            end
          end
        end
        true
      end

      def unlink(pid)
        pid.tell UnLink.new(@Pid) if @Linked.delete pid
        true
      end

      def linked?(pid)
        @Linked.include? pid
      end

      def monitor(pid)
        # *monitoring*    *monitored*
        # send Monitor
        # terminated?
        #                 terminate before getting Monitor
        #                 drain signals including the Monitor
        reference              = Reference.new
        @Monitoring[reference] = pid
        if pid.terminated.resolved?
          # always return no-proc when terminated
          tell DownSignal.new(pid, reference, NoActor.new(pid))
        else
          # otherwise let it race
          pid.tell Monitor.new(@Pid, reference)
          # no race, it cannot get anything else than NoActor
          tell DownSignal.new(pid, reference, NoActor.new(pid)) if pid.terminated.resolved?
        end
        reference
      end

      def demonitor(reference, *options)
        info  = options.delete :info
        flush = options.delete :flush
        raise ArgumentError, "bad options #{options}" unless options.empty?

        pid          = @Monitoring.delete reference
        demonitoring = !!pid
        pid.tell DeMonitor.new @Pid, reference if demonitoring

        if flush
          # remove (one) down message having reference from mailbox
          flushed = demonitoring ? !!@Mailbox.try_pop_matching(And[DownSignal, -> m { m.reference == reference }]) : false
          return info ? !flushed : true
        end

        if info
          return false unless demonitoring

          if @Mailbox.peek_matching(And[DownSignal, -> m { m.reference == reference }])
            @MonitoringLateDelivery[reference] = pid # allow to deliver the message once
            return false
          end
        end

        return true
      end

      def monitoring?(reference)
        @Monitoring.include? reference
      end

      def spawn(*args,
                type:,
                channel:,
                environment:,
                name:,
                link:,
                monitor:,
                executor:,
                &body)
        actor = ErlangActor.create type, channel, environment, name, executor
        pid   = actor.pid
        link pid if link
        ref = (monitor pid if monitor)
        actor.run(*args, &body)
        monitor ? [pid, ref] : pid
      end

      def reply_resolution(fulfilled, value, reason)
        return false unless @reply
        return !!@reply.resolve(fulfilled, value, reason, false)
      end

      def terminate(pid = nil, reason, value: nil)
        if pid
          # has to send it to itself even if pid equals self.pid
          if reason == :kill
            pid.tell Kill.new(@Pid)
          else
            pid.tell Terminate.new(@Pid, reason, false)
          end
        else
          terminate_self(reason, value)
        end
      end

      private

      def canonical_rules(rules, timeout, timeout_value, given_block)
        block = given_block || -> v { v }
        case rules.size
        when 0
          rules.push(on(ANY, &block))
        when 1
          matcher = rules.first
          if matcher.is_a?(::Array) && matcher.size == 2
            return ArgumentError.new 'a block cannot be given if full rules are used' if given_block
          else
            rules.replace([on(matcher, &block)])
          end
        else
          return ArgumentError.new 'a block cannot be given if full rules are used' if given_block
        end

        if timeout
          # TIMEOUT rule has to be first, to prevent any picking it up ANY
          has_timeout = nil
          i           = rules.size
          rules.reverse_each do |r, _|
            i -= 1
            if r == TIMEOUT
              has_timeout = i
              break
            end
          end

          rules.unshift(has_timeout ? rules[has_timeout] : on(TIMEOUT, timeout_value))
        end
        nil
      end

      def eval_task(message, job)
        if job.is_a? Proc
          @Environment.instance_exec message, &job
        else
          job
        end
      end

      def send_exit_messages(reason)
        @Linked.each do |pid|
          pid.tell Terminate.new(@Pid, reason)
        end.clear
        @Monitors.each do |reference, pid|
          pid.tell DownSignal.new(@Pid, reference, reason)
        end.clear
      end

      def asked?
        !!@reply
      end

      def clean_reply(reason = NoReply)
        if @reply
          @reply.reject(reason, false)
          @reply = nil
        end
      end

      def consume_signal(message)
        if AbstractSignal === message
          case message
          when Ask
            @reply = message.probe
            message.message
          when Link
            @Linked.add message.from
            NOTHING
          when UnLink
            @Linked.delete message.from
            NOTHING
          when Monitor
            @Monitors[message.reference] = message.from
            NOTHING
          when DeMonitor
            @Monitors.delete message.reference
            NOTHING
          when Kill
            terminate :killed
          when DownSignal
            if @Monitoring.delete(message.reference) || @MonitoringLateDelivery.delete(message.reference)
              # put into a queue
              return Down.new(message.from, message.reference, message.info)
            end

            # ignore down message if no longer monitoring, and following case
            #
            # *monitoring*    *monitored*
            # send Monitor
            #                 terminate
            # terminated?
            #                 drain signals # generates second DOWN which is dropped here
            # already reported as :noproc
            NOTHING
          when Terminate
            consume_exit message
          else
            raise "unknown message #{message}"
          end
        else
          # regular message
          message
        end
      end

      def consume_exit(exit_message)
        from, reason = exit_message
        if !exit_message.link_terminated || @Linked.delete(from)
          case reason
          when :normal
            if @trap
              Terminated.new from, reason
            else
              if from == @Pid
                terminate :normal
              else
                NOTHING # do nothing
              end
            end
          else
            if @trap
              Terminated.new from, reason
            else
              terminate reason
            end
          end
        else
          # *link*          *exiting*
          # send Link
          #                 terminate
          # terminated?
          #                 drain signals # generates second Terminated which is dropped here
          # already processed exit message, do nothing
          NOTHING
        end
      end

      def initial_signal_consumption
        while true
          message = @Mailbox.try_pop
          break unless message
          consume_signal(message) == NOTHING or raise 'it was not consumable signal'
        end
      end

      def terminate_self(reason, value)
        raise NotImplementedError
      end

      def after_termination(final_reason)
        log Logger::DEBUG, @Pid, terminated: final_reason
        clean_reply NoActor.new(@Pid)
        while true
          message = @Mailbox.try_pop NOTHING
          break if message == NOTHING
          case message
          when Monitor
            # The actor is terminated so we must return NoActor,
            # even though we still know the reason.
            # Otherwise it would return different reasons non-deterministically.
            message.from.tell DownSignal.new(@Pid, message.reference, NoActor.new(@Pid))
          when Link
            # same as for Monitor
            message.from.tell NoActor.new(@Pid)
          when Ask
            message.probe.reject(NoActor.new(@Pid), false)
          else
            # normal messages and other signals are thrown away
          end
        end
        @Mailbox = nil
      end
    end

    private_constant :AbstractActor

    class OnPool < AbstractActor

      def initialize(channel, environment, name, executor)
        super channel, environment, name, executor
        @Executor       = executor
        @behaviour      = []
        @keep_behaviour = false
      end

      def run(*args, &body)
        body ||= -> { start }

        initial_signal_consumption
        inner_run(*args, &body).
            run(Run::TEST).
            then(&method(:after_termination)).
            rescue { |e| log Logger::ERROR, e }
      end

      def receive(*rules, timeout: nil, timeout_value: nil, keep: false, &given_block)
        clean_reply
        err = canonical_rules rules, timeout, timeout_value, given_block
        raise err if err

        @keep_behaviour = keep
        @timeout        = timeout
        @behaviour      = rules
        throw JUMP, [RECEIVE]
      end

      private

      def terminate_self(reason, value)
        throw JUMP, [TERMINATE, reason, value]
      end

      def inner_run(*args, &body)
        first       = !!body
        future_body = -> message, _actor do
          kind, reason, value =
              if message.is_a?(::Array) && message.first == TERMINATE
                message
              else
                begin
                  catch(JUMP) do
                    [NOTHING,
                     :normal,
                     first ? @Environment.instance_exec(*args, &body) : apply_behaviour(message)]
                  end
                rescue => e
                  [TERMINATE, e, nil]
                end
              end

          case kind
          when TERMINATE
            send_exit_messages reason
            @Terminated.resolve(reason == :normal, value, reason)
            reason
          when RECEIVE
            Run[inner_run]
          when NOTHING
            if @behaviour.empty?
              send_exit_messages reason
              @Terminated.resolve(reason == :normal, value, reason)
              reason
            else
              Run[inner_run]
            end
          else
            raise "bad kind: #{kind.inspect}"
          end
        end

        if first
          Promises.future_on(@Executor, nil, self, &future_body)
        else
          internal_receive.run(Run::TEST).then(self, &future_body)
        end
      end

      def internal_receive
        raise if @behaviour.empty?
        rules_matcher  = Or[*@behaviour.map(&:first)]
        matcher        = -> m { m.is_a?(Ask) ? rules_matcher === m.message : rules_matcher === m }
        start          = nil
        message_future = case @timeout
                         when 0
                           Promises.fulfilled_future @Mailbox.try_pop_matching(matcher, TIMEOUT)
                         when Numeric
                           pop   = @Mailbox.pop_op_matching(matcher)
                           start = Concurrent.monotonic_time
                           # FIXME (pitr-ch 30-Jan-2019): the scheduled future should be cancelled
                           (Promises.schedule(@timeout) { TIMEOUT } | pop).then(pop) do |message, p|
                             if message == TIMEOUT && !p.resolve(true, TIMEOUT, nil, false)
                               # timeout raced with probe resolution, take the value instead
                               p.value
                             else
                               message
                             end
                           end
                         when nil
                           @Mailbox.pop_op_matching(matcher)
                         else
                           raise
                         end

        message_future.then(start, self) do |message, s, _actor|
          log Logger::DEBUG, pid, got: message
          catch(JUMP) do
            if (message = consume_signal(message)) == NOTHING
              @timeout = [@timeout + s - Concurrent.monotonic_time, 0].max if s
              Run[internal_receive]
            else
              message
            end
          end
        end
      end

      def apply_behaviour(message)
        @behaviour.each do |rule, job|
          if rule === message
            @behaviour = [] unless @keep_behaviour
            return eval_task(message, job)
          end
        end
        raise 'should not reach'
      end
    end

    private_constant :OnPool

    class OnThread < AbstractActor
      def initialize(channel, environment, name, executor)
        super channel, environment, name, executor
        @Thread = nil
      end

      TERMINATE = Module.new
      private_constant :TERMINATE

      def run(*args, &body)
        initial_signal_consumption
        @Thread = Thread.new(@Terminated, self) do |terminated, _actor| # sync point
          Thread.abort_on_exception = true

          final_reason = begin
            reason, value = catch(TERMINATE) do
              [:normal, @Environment.instance_exec(*args, &body)]
            end
            send_exit_messages reason
            terminated.resolve(reason == :normal, value, reason)
            reason
          rescue => e
            send_exit_messages e
            terminated.reject e
            e
          end

          after_termination final_reason
          @Thread = nil
        end
      end

      def receive(*rules, timeout: nil, timeout_value: nil, &given_block)
        clean_reply

        err = canonical_rules rules, timeout, timeout_value, given_block
        raise err if err

        rules_matcher = Or[*rules.map(&:first)]
        matcher       = -> m { m.is_a?(Ask) ? rules_matcher === m.message : rules_matcher === m }
        while true
          message = @Mailbox.pop_matching(matcher, timeout, TIMEOUT)
          log Logger::DEBUG, pid, got: message
          unless (message = consume_signal(message)) == NOTHING
            rules.each do |rule, job|
              return eval_task(message, job) if rule === message
            end
          end
        end
      end

      private

      def terminate_self(reason, value)
        throw TERMINATE, [reason, value]
      end
    end

    private_constant :OnThread

    class AbstractSignal < Synchronization::Object
      safe_initialization!
    end

    private_constant :AbstractSignal

    class Ask < AbstractSignal
      attr_reader :message, :probe

      def initialize(message, probe)
        super()
        @message = message
        @probe   = probe
        raise ArgumentError, 'probe is not Resolvable' unless probe.is_a? Promises::Resolvable
      end
    end

    private_constant :Ask

    module HasFrom

      # @return [Pid]
      attr_reader :from

      # @!visibility private
      def initialize(from)
        # noinspection RubySuperCallWithoutSuperclassInspection
        super()
        @from = from
      end

      # @return [true, false]
      def ==(o)
        o.class == self.class && o.from == @from
      end

      alias_method :eql?, :==

      # @return [Integer]
      def hash
        @from.hash
      end
    end

    private_constant :HasFrom

    module HasReason
      include HasFrom

      # @return [Object]
      attr_reader :reason

      # @!visibility private
      def initialize(from, reason)
        # noinspection RubySuperCallWithoutSuperclassInspection
        super from
        @reason = reason
      end

      # @return [::Array(Pid, Object)]
      def to_ary
        [@from, @reason]
      end

      # @return [true, false]
      def ==(o)
        # noinspection RubySuperCallWithoutSuperclassInspection
        super(o) && o.reason == self.reason
      end

      # @return [Integer]
      def hash
        [@from, @reason].hash
      end
    end

    private_constant :HasReason

    module HasReference
      include HasFrom

      # @return [Reference]
      attr_reader :reference

      # @!visibility private
      def initialize(from, reference)
        # noinspection RubySuperCallWithoutSuperclassInspection
        super from
        @reference = reference
      end

      # @return [::Array(Pid, Reference)]
      def to_ary
        [@from, @reference]
      end

      # @return [true, false]
      def ==(o)
        # noinspection RubySuperCallWithoutSuperclassInspection
        super(o) && o.reference == self.reference
      end

      # @return [Integer]
      def hash
        [@from, @reference].hash
      end
    end

    private_constant :HasReference

    class Terminate < AbstractSignal
      include HasReason

      attr_reader :link_terminated

      def initialize(from, reason, link_terminated = true)
        super from, reason
        @link_terminated = link_terminated
      end
    end

    private_constant :Terminate

    class Kill < AbstractSignal
      include HasFrom
    end

    private_constant :Kill

    class Link < AbstractSignal
      include HasFrom
    end

    private_constant :Link

    class UnLink < AbstractSignal
      include HasFrom
    end

    private_constant :UnLink

    class Monitor < AbstractSignal
      include HasReference
    end

    private_constant :Monitor

    class DeMonitor < AbstractSignal
      include HasReference
    end

    private_constant :DeMonitor

    # A message send when actor terminates.
    class Terminated
      # @return [Pid]
      attr_reader :from
      # @return [Object]
      attr_reader :reason

      # @!visibility private
      def initialize(from, reason)
        # noinspection RubySuperCallWithoutSuperclassInspection
        @from   = from
        @reason = reason
      end

      # @return [::Array(Pid, Object)]
      def to_ary
        [@from, @reason]
      end

      # @return [true, false]
      def ==(o)
        o.class == self.class && o.from == @from && o.reason == self.reason
      end

      alias_method :eql?, :==

      # @return [Integer]
      def hash
        [@from, @reason].hash
      end
    end

    class DownSignal < AbstractSignal
      include HasReference

      # @return [Object]
      attr_reader :info

      # @!visibility private
      def initialize(from, reference, info)
        super from, reference
        @info = info
      end

      # @return [::Array(Pis, Reference, Object)]
      def to_ary
        [@from, @reference, @info]
      end

      # @return [true, false]
      def ==(o)
        super(o) && o.info == self.info
      end

      # @return [Integer]
      def hash
        to_ary.hash
      end
    end

    private_constant :DownSignal

    # A message send by a monitored actor when terminated.
    class Down
      # @return [Pid]
      attr_reader :from
      # @return [Reference]
      attr_reader :reference
      # @return [Object]
      attr_reader :info

      # @!visibility private
      def initialize(from, reference, info)
        @from      = from
        @reference = reference
        @info      = info
      end

      # @return [::Array(Pis, Reference, Object)]
      def to_ary
        [@from, @reference, @info]
      end

      # @return [true, false]
      def ==(o)
        o.class == self.class && o.from == @from && o.reference == @reference && o.info == @info
      end

      alias_method :eql?, :==

      # @return [Integer]
      def hash
        to_ary.hash
      end
    end

    # Abstract error class for ErlangActor errors.
    class Error < Concurrent::Error
    end

    # An error used when actor tries to link or monitor terminated actor.
    class NoActor < Error
      # @return [Pid]
      attr_reader :pid

      # @param [Pid] pid
      # @return [self]
      def initialize(pid = nil)
        super(pid.to_s)
        @pid = pid
      end

      # @return [true, false]
      def ==(o)
        o.class == self.class && o.pid == self.pid
      end

      alias_method :eql?, :==

      # @return [Integer]
      def hash
        pid.hash
      end
    end

    # An error used when actor is asked but no reply was given or
    # when the actor terminates before it gives a reply.
    class NoReply < Error
    end

    # @!visibility private
    def self.create(type, channel, environment, name, executor)
      actor = KLASS_MAP.fetch(type).new(channel, environment, name, executor)
    ensure
      log Logger::DEBUG, actor.pid, created: caller[1] if actor
    end

    KLASS_MAP = {
        on_thread: OnThread,
        on_pool:   OnPool,
        OnThread   => OnThread,
        OnPool     => OnPool,
    }
    private_constant :KLASS_MAP
  end
end
