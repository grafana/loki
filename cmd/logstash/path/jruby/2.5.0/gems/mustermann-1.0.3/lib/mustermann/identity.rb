# frozen_string_literal: true
require 'mustermann'
require 'mustermann/pattern'
require 'mustermann/ast/node'

module Mustermann
  # Matches strings that are identical to the pattern.
  #
  # @example
  #   Mustermann.new('/:foo', type: :identity) === '/bar' # => false
  #
  # @see Mustermann::Pattern
  # @see file:README.md#identity Syntax description in the README
  class Identity < Pattern
    include Concat::Native
    register :identity

    # @param (see Mustermann::Pattern#===)
    # @return (see Mustermann::Pattern#===)
    # @see (see Mustermann::Pattern#===)
    def ===(string)
      unescape(string) == @string
    end

    # @param (see Mustermann::Pattern#peek_size)
    # @return (see Mustermann::Pattern#peek_size)
    # @see (see Mustermann::Pattern#peek_size)
    def peek_size(string)
      return unless unescape(string).start_with? @string
      return @string.size if string.start_with? @string # optimization
      @string.each_char.with_index.inject(0) do |count, (char, index)|
        char_size = 1
        escaped   = @@uri.escape(char, /./)
        char_size = escaped.size if string[index, escaped.size].downcase == escaped.downcase
        count + char_size
      end
    end

    # URI templates support generating templates (the logic is quite complex, though).
    #
    # @example (see Mustermann::Pattern#to_templates)
    # @param (see Mustermann::Pattern#to_templates)
    # @return (see Mustermann::Pattern#to_templates)
    # @see Mustermann::Pattern#to_templates
    def to_templates
      [@@uri.escape(to_s)]
    end

    # Generates an AST so it's compatible with {Mustermann::AST::Pattern}.
    # Not used internally by {Mustermann::Identity}.
    # @!visibility private
    def to_ast
      payload = @string.each_char.with_index.map { |c, i| AST::Node[c == ?/ ? :separator : :char].new(c, start: i, stop: i+1) }
      AST::Node[:root].new(payload, pattern: @string, start: 0, stop: @string.length)
    end

    # Identity patterns support expanding.
    #
    # This implementation does not use {Mustermann::Expander} internally to save memory and
    # compilation time.
    #
    # @example (see Mustermann::Pattern#expand)
    # @param (see Mustermann::Pattern#expand)
    # @return (see Mustermann::Pattern#expand)
    # @raise (see Mustermann::Pattern#expand)
    # @see Mustermann::Pattern#expand
    # @see Mustermann::Expander
    def expand(behavior = nil, values = {})
      return to_s if values.empty? or behavior == :ignore
      raise ExpandError,    "cannot expand with keys %p" % values.keys.sort if behavior == :raise
      raise ArgumentError,  "unknown behavior %p"        % behavior         if behavior != :append
      params    = values.map { |key, value| @@uri.escape(key.to_s) + "=" + @@uri.escape(value.to_s, /[^\w]/) }
      separator = @string.include?(??) ? ?& : ??
      @string + separator + params.join(?&)
    end
  end
end
