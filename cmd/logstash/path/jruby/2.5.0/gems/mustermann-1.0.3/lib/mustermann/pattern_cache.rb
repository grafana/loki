# frozen_string_literal: true
require 'set'
require 'thread'
require 'mustermann'

module Mustermann
  # A simple, persistent cache for creating repositories.
  #
  # @example
  #   require 'mustermann/pattern_cache'
  #   cache = Mustermann::PatternCache.new
  #
  #   # use this instead of Mustermann.new
  #   pattern = cache.create_pattern("/:name", type: :rails)
  #
  # @note
  #   {Mustermann::Pattern.new} (which is used by {Mustermann.new}) will reuse instances that have
  #   not yet been garbage collected. You only need an extra cache if you do not keep a reference to
  #   the patterns around.
  #
  # @api private
  class PatternCache
    # @param [Hash] pattern_options default options used for {#create_pattern}
    def initialize(**pattern_options)
      @cached          = Set.new
      @mutex           = Mutex.new
      @pattern_options = pattern_options
    end

    # @param (see Mustermann.new)
    # @return (see Mustermann.new)
    # @raise (see Mustermann.new)
    # @see Mustermann.new
    def create_pattern(string, **pattern_options)
      pattern = Mustermann.new(string, **pattern_options, **@pattern_options)
      @mutex.synchronize { @cached.add(pattern) } unless @cached.include? pattern
      pattern
    end

    # Removes all pattern instances from the cache.
    def clear
      @mutex.synchronize { @cached.clear }
    end

    # @return [Integer] number of currently cached patterns
    def size
      @mutex.synchronize { @cached.size }
    end
  end
end
