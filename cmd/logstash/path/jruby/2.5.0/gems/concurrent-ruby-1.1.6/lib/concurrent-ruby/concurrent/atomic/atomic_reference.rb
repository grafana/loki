require 'concurrent/synchronization'
require 'concurrent/utility/engine'
require 'concurrent/atomic_reference/numeric_cas_wrapper'

# Shim for TruffleRuby::AtomicReference
if Concurrent.on_truffleruby? && !defined?(TruffleRuby::AtomicReference)
  # @!visibility private
  module TruffleRuby
    AtomicReference = Truffle::AtomicReference
  end
end

module Concurrent

  # Define update methods that use direct paths
  #
  # @!visibility private
  # @!macro internal_implementation_note
  module AtomicDirectUpdate

    # @!macro atomic_reference_method_update
    #
    #   Pass the current value to the given block, replacing it
    #   with the block's result. May retry if the value changes
    #   during the block's execution.
    #
    #   @yield [Object] Calculate a new value for the atomic reference using
    #     given (old) value
    #   @yieldparam [Object] old_value the starting value of the atomic reference
    #   @return [Object] the new value
    def update
      true until compare_and_set(old_value = get, new_value = yield(old_value))
      new_value
    end

    # @!macro atomic_reference_method_try_update
    #
    #   Pass the current value to the given block, replacing it
    #   with the block's result. Return nil if the update fails.
    #
    #   @yield [Object] Calculate a new value for the atomic reference using
    #     given (old) value
    #   @yieldparam [Object] old_value the starting value of the atomic reference
    #   @note This method was altered to avoid raising an exception by default.
    #     Instead, this method now returns `nil` in case of failure. For more info,
    #     please see: https://github.com/ruby-concurrency/concurrent-ruby/pull/336
    #   @return [Object] the new value, or nil if update failed
    def try_update
      old_value = get
      new_value = yield old_value

      return unless compare_and_set old_value, new_value

      new_value
    end

    # @!macro atomic_reference_method_try_update!
    #
    #   Pass the current value to the given block, replacing it
    #   with the block's result. Raise an exception if the update
    #   fails.
    #
    #   @yield [Object] Calculate a new value for the atomic reference using
    #     given (old) value
    #   @yieldparam [Object] old_value the starting value of the atomic reference
    #   @note This behavior mimics the behavior of the original
    #     `AtomicReference#try_update` API. The reason this was changed was to
    #     avoid raising exceptions (which are inherently slow) by default. For more
    #     info: https://github.com/ruby-concurrency/concurrent-ruby/pull/336
    #   @return [Object] the new value
    #   @raise [Concurrent::ConcurrentUpdateError] if the update fails
    def try_update!
      old_value = get
      new_value = yield old_value
      unless compare_and_set(old_value, new_value)
        if $VERBOSE
          raise ConcurrentUpdateError, "Update failed"
        else
          raise ConcurrentUpdateError, "Update failed", ConcurrentUpdateError::CONC_UP_ERR_BACKTRACE
        end
      end
      new_value
    end
  end

  require 'concurrent/atomic_reference/mutex_atomic'

  # @!macro atomic_reference
  #
  #   An object reference that may be updated atomically. All read and write
  #   operations have java volatile semantic.
  #
  #   @!macro thread_safe_variable_comparison
  #
  #   @see http://docs.oracle.com/javase/8/docs/api/java/util/concurrent/atomic/AtomicReference.html
  #   @see http://docs.oracle.com/javase/8/docs/api/java/util/concurrent/atomic/package-summary.html
  #
  #   @!method initialize(value = nil)
  #     @!macro atomic_reference_method_initialize
  #       @param [Object] value The initial value.
  #
  #   @!method get
  #     @!macro atomic_reference_method_get
  #       Gets the current value.
  #       @return [Object] the current value
  #
  #   @!method set(new_value)
  #     @!macro atomic_reference_method_set
  #       Sets to the given value.
  #       @param [Object] new_value the new value
  #       @return [Object] the new value
  #
  #   @!method get_and_set(new_value)
  #     @!macro atomic_reference_method_get_and_set
  #       Atomically sets to the given value and returns the old value.
  #       @param [Object] new_value the new value
  #       @return [Object] the old value
  #
  #   @!method compare_and_set(old_value, new_value)
  #     @!macro atomic_reference_method_compare_and_set
  #
  #       Atomically sets the value to the given updated value if
  #       the current value == the expected value.
  #
  #       @param [Object] old_value the expected value
  #       @param [Object] new_value the new value
  #
  #       @return [Boolean] `true` if successful. A `false` return indicates
  #       that the actual value was not equal to the expected value.
  #
  #   @!method update
  #     @!macro atomic_reference_method_update
  #
  #   @!method try_update
  #     @!macro atomic_reference_method_try_update
  #
  #   @!method try_update!
  #     @!macro atomic_reference_method_try_update!


  # @!macro internal_implementation_note
  class ConcurrentUpdateError < ThreadError
    # frozen pre-allocated backtrace to speed ConcurrentUpdateError
    CONC_UP_ERR_BACKTRACE = ['backtrace elided; set verbose to enable'].freeze
  end

  # @!macro internal_implementation_note
  AtomicReferenceImplementation = case
                                  when Concurrent.on_cruby? && Concurrent.c_extensions_loaded?
                                    # @!visibility private
                                    # @!macro internal_implementation_note
                                    class CAtomicReference
                                      include AtomicDirectUpdate
                                      include AtomicNumericCompareAndSetWrapper
                                      alias_method :compare_and_swap, :compare_and_set
                                    end
                                    CAtomicReference
                                  when Concurrent.on_jruby?
                                    # @!visibility private
                                    # @!macro internal_implementation_note
                                    class JavaAtomicReference
                                      include AtomicDirectUpdate
                                    end
                                    JavaAtomicReference
                                  when Concurrent.on_truffleruby?
                                    class TruffleRubyAtomicReference < TruffleRuby::AtomicReference
                                      include AtomicDirectUpdate
                                      alias_method :value, :get
                                      alias_method :value=, :set
                                      alias_method :compare_and_swap, :compare_and_set
                                      alias_method :swap, :get_and_set
                                    end
                                  when Concurrent.on_rbx?
                                    # @note Extends `Rubinius::AtomicReference` version adding aliases
                                    #   and numeric logic.
                                    #
                                    # @!visibility private
                                    # @!macro internal_implementation_note
                                    class RbxAtomicReference < Rubinius::AtomicReference
                                      alias_method :_compare_and_set, :compare_and_set
                                      include AtomicDirectUpdate
                                      include AtomicNumericCompareAndSetWrapper
                                      alias_method :value, :get
                                      alias_method :value=, :set
                                      alias_method :swap, :get_and_set
                                      alias_method :compare_and_swap, :compare_and_set
                                    end
                                    RbxAtomicReference
                                  else
                                    MutexAtomicReference
                                  end
  private_constant :AtomicReferenceImplementation

  # @!macro atomic_reference
  class AtomicReference < AtomicReferenceImplementation

    # @return [String] Short string representation.
    def to_s
      format '%s value:%s>', super[0..-2], get
    end

    alias_method :inspect, :to_s
  end
end
