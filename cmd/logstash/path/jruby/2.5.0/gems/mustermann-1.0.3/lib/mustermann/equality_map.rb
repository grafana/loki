# frozen_string_literal: true
module Mustermann
  # A simple wrapper around ObjectSpace::WeakMap that allows matching keys by equality rather than identity.
  # Used for caching. Note that `fetch` is not guaranteed to return the object, even if it has not been
  # garbage collected yet, especially when used concurrently. Therefore, the block passed to `fetch` has to
  # be idempotent.
  #
  # @example
  #   class ExpensiveComputation
  #     @map = Mustermann::EqualityMap.new
  #
  #     def self.new(*args)
  #       @map.fetch(args) { super }
  #     end
  #   end
  #
  # @see #fetch
  class EqualityMap
    attr_reader :map

    def self.new
      defined?(ObjectSpace::WeakMap) ? super : {}
    end

    def initialize
      @keys = {}
      @map  = ObjectSpace::WeakMap.new
    end

    # @param [#hash] key for caching
    # @yield block that will be called to populate entry if missing (has to be idempotent)
    # @return value stored in map or result of block
    def fetch(key)
      identity = @keys[key.hash]
      if identity == key
        key = identity
      elsif key.frozen?
        key = key.dup
      end

      # it is ok that this is not thread-safe, worst case it has double cost in
      # generating, object equality is not guaranteed anyways
      @map[key] ||= track(key, yield)
    end

    # @param [#hash] key for identifying the object
    # @param [Object] object to be stored
    # @return [Object] same as the second parameter
    def track(key, object)
      object = object.dup if object.frozen?
      ObjectSpace.define_finalizer(object, finalizer(key.hash))
      @keys[key.hash] = key
      object
    end

    # Finalizer proc needs to be generated in different scope so it doesn't keep a reference to the object.
    #
    # @param [Integer] hash for key
    # @return [Proc] finalizer callback
    def finalizer(hash)
      proc { @keys.delete(hash) }
    end

    private :track, :finalizer
  end
end
