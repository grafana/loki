module Concurrent
  # noinspection RubyInstanceVariableNamingConvention
  module Synchronization

    # @!visibility private
    # @!macro internal_implementation_note
    module ConditionSignalling
      protected

      def ns_signal
        @__Condition__.signal
        self
      end

      def ns_broadcast
        @__Condition__.broadcast
        self
      end
    end


    # @!visibility private
    # @!macro internal_implementation_note
    class MutexLockableObject < AbstractLockableObject
      include ConditionSignalling

      safe_initialization!

      def initialize(*defaults)
        super(*defaults)
        @__Lock__      = ::Mutex.new
        @__Condition__ = ::ConditionVariable.new
      end

      protected

      def synchronize
        if @__Lock__.owned?
          yield
        else
          @__Lock__.synchronize { yield }
        end
      end

      def ns_wait(timeout = nil)
        @__Condition__.wait @__Lock__, timeout
        self
      end
    end

    # @!visibility private
    # @!macro internal_implementation_note
    class MonitorLockableObject < AbstractLockableObject
      include ConditionSignalling

      safe_initialization!

      def initialize(*defaults)
        super(*defaults)
        @__Lock__      = ::Monitor.new
        @__Condition__ = @__Lock__.new_cond
      end

      protected

      def synchronize # TODO may be a problem with lock.synchronize { lock.wait }
        @__Lock__.synchronize { yield }
      end

      def ns_wait(timeout = nil)
        @__Condition__.wait timeout
        self
      end
    end
  end
end
