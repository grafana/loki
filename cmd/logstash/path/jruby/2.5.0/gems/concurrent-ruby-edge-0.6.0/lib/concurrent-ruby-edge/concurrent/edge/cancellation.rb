require 'concurrent/concern/deprecation'

module Concurrent

  # TODO (pitr-ch 27-Mar-2016): cooperation with mutex, condition, select etc?
  # TODO (pitr-ch 10-Dec-2018): integrate with enumerator?
  # token.cancelable(array.each_with_index).each do |v, i|
  #   # stops iterating when cancelled
  # end
  # token.cancelable(array).each_with_index do |v, i|
  #   # stops iterating when cancelled
  # end

  # The Cancellation abstraction provides cooperative cancellation.
  #
  # The standard methods `Thread#raise` of `Thread#kill` available in Ruby
  # are very dangerous (see linked the blog posts bellow).
  # Therefore concurrent-ruby provides an alternative.
  # * <https://jvns.ca/blog/2015/11/27/why-rubys-timeout-is-dangerous-and-thread-dot-raise-is-terrifying/>
  # * <http://www.mikeperham.com/2015/05/08/timeout-rubys-most-dangerous-api/>
  # * <http://blog.headius.com/2008/02/rubys-threadraise-threadkill-timeoutrb.html>
  #
  # It provides an object which represents a task which can be executed,
  # the task has to get the reference to the object and periodically cooperatively check that it is not cancelled.
  # Good practices to make tasks cancellable:
  # * check cancellation every cycle of a loop which does significant work,
  # * do all blocking actions in a loop with a timeout then on timeout check cancellation
  #   and if ok block again with the timeout
  #
  # The idea was inspired by <https://msdn.microsoft.com/en-us/library/dd537607(v=vs.110).aspx>
  # @!macro warn.edge
  #
  # {include:file:docs-source/cancellation.out.md}
  class Cancellation < Synchronization::Object
    safe_initialization!

    # Create Cancellation which will cancel itself in given time
    #
    # @!macro promises.param.intended_time
    # @return [Cancellation]
    def self.timeout(intended_time)
      new Concurrent::Promises.schedule(intended_time)
    end

    # Creates the cancellation object.
    #
    # @param [Promises::Future, Promises::Event] origin of the cancellation.
    #   When it is resolved the cancellation is canceled.
    # @example
    #   cancellation, origin = Concurrent::Cancellation.new
    # @see #to_ary
    def initialize(origin = Promises.resolvable_event)
      super()
      @Origin = origin
    end

    # Allow to multi-assign the Cancellation object
    # @return [Array(Cancellation, Promises::Future), Array(Cancellation, Promises::Event)]
    # @example
    #   cancellation         = Concurrent::Cancellation.new
    #   cancellation, origin = Concurrent::Cancellation.new
    def to_ary
      [self, @Origin]
    end

    # The event or future which is the origin of the cancellation
    # @return [Promises::Future, Promises::Event]
    def origin
      @Origin
    end

    # Is the cancellation cancelled?
    # Respective, was the origin of the cancellation resolved.
    # @return [true, false]
    def canceled?
      @Origin.resolved?
    end

    # Raise error when cancelled
    # @param [#exception] error to be risen
    # @raise the error
    # @return [self]
    def check!(error = CancelledOperationError)
      raise error if canceled?
      self
    end

    # Creates a new Cancellation which is cancelled when first
    # of the supplied cancellations or self is cancelled.
    #
    # @param [Cancellation] cancellations to combine
    # @return [Cancellation] new cancellation
    def join(*cancellations)
      Cancellation.new Promises.any_event(*[@Origin, *cancellations.map(&:origin)])
    end

    # Short string representation.
    # @return [String]
    def to_s
      format '%s %s>', super[0..-2], canceled? ? 'canceled' : 'pending'
    end

    alias_method :inspect, :to_s
  end
end
