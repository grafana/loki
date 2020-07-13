# frozen_string_literal: true
module Mustermann
  # Fakes MatchData for patterns that do not support capturing.
  # @see http://ruby-doc.org/core-2.0/MatchData.html MatchData
  class SimpleMatch
    # @api private
    def initialize(string = "", names: [], captures: [])
      @string   = string.dup
      @names    = names
      @captures = captures
    end

    # @return [String] the string that was matched against
    def to_s
      @string.dup
    end

    # @return [Array<String>] empty array for imitating MatchData interface
    def names
      @names.dup
    end

    # @return [Array<String>] empty array for imitating MatchData interface
    def captures
      @captures.dup
    end

    # @return [nil] imitates MatchData interface
    def [](*args)
      args.map! do |arg|
        next arg unless arg.is_a? Symbol or arg.is_a? String
        names.index(arg.to_s)
      end
      @captures[*args]
    end

    # @!visibility private
    def +(other)
      SimpleMatch.new(@string + other.to_s,
          names:    @names    + other.names,
          captures: @captures + other.captures)
    end

    # @return [String] string representation
    def inspect
      "#<%p %p>" % [self.class, @string]
    end
  end
end
