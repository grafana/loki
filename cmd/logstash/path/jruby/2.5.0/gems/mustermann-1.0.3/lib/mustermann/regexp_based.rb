# frozen_string_literal: true
require 'mustermann/pattern'
require 'forwardable'

module Mustermann
  # Superclass for patterns that internally compile to a regular expression.
  # @see Mustermann::Pattern
  # @abstract
  class RegexpBased < Pattern
    # @return [Regexp] regular expression equivalent to the pattern.
    attr_reader :regexp
    alias_method :to_regexp, :regexp

    # @param (see Mustermann::Pattern#initialize)
    # @return (see Mustermann::Pattern#initialize)
    # @see (see Mustermann::Pattern#initialize)
    def initialize(string, **options)
      super
      regexp       = compile(**options)
      @peek_regexp = /\A#{regexp}/
      @regexp      = /\A#{regexp}\Z/
    end

    # @param (see Mustermann::Pattern#peek_size)
    # @return (see Mustermann::Pattern#peek_size)
    # @see (see Mustermann::Pattern#peek_size)
    def peek_size(string)
      return unless match = peek_match(string)
      match.to_s.size
    end

    # @param (see Mustermann::Pattern#peek_match)
    # @return (see Mustermann::Pattern#peek_match)
    # @see (see Mustermann::Pattern#peek_match)
    def peek_match(string)
      @peek_regexp.match(string)
    end

    extend Forwardable
    def_delegators :regexp, :===, :=~, :match, :names, :named_captures

    def compile(**options)
      raise NotImplementedError, 'subclass responsibility'
    end

    private :compile
  end
end
