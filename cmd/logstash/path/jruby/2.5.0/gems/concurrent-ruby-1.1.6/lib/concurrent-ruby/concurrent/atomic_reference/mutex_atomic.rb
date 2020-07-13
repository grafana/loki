module Concurrent

  # @!visibility private
  # @!macro internal_implementation_note
  class MutexAtomicReference < Synchronization::LockableObject
    include AtomicDirectUpdate
    include AtomicNumericCompareAndSetWrapper
    alias_method :compare_and_swap, :compare_and_set

    # @!macro atomic_reference_method_initialize
    def initialize(value = nil)
      super()
      synchronize { ns_initialize(value) }
    end

    # @!macro atomic_reference_method_get
    def get
      synchronize { @value }
    end
    alias_method :value, :get

    # @!macro atomic_reference_method_set
    def set(new_value)
      synchronize { @value = new_value }
    end
    alias_method :value=, :set

    # @!macro atomic_reference_method_get_and_set
    def get_and_set(new_value)
      synchronize do
        old_value = @value
        @value = new_value
        old_value
      end
    end
    alias_method :swap, :get_and_set

    # @!macro atomic_reference_method_compare_and_set
    def _compare_and_set(old_value, new_value)
      synchronize do
        if @value.equal? old_value
          @value = new_value
          true
        else
          false
        end
      end
    end

    protected

    def ns_initialize(value)
      @value = value
    end
  end
end
