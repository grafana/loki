# frozen_string_literal: true
module Mustermann
  # Class for pattern objects that are a concatenation of other patterns.
  # @see Mustermann::Pattern#+
  class Concat < Composite
    # Mixin for patterns to support native concatenation.
    # @!visibility private
    module Native
      # @see Mustermann::Pattern#+
      # @!visibility private
      def +(other)
        other &&= Mustermann.new(other, type: :identity, **options)
        if (patterns = look_ahead(other)) && !patterns.empty?
          concat = (self + patterns.inject(:+))
          concat + other.patterns.slice(patterns.length..-1).inject(:+)
        else
          return super unless native = native_concat(other)
          self.class.new(native, **options)
        end
      end

      # @!visibility private
      def look_ahead(other)
        return unless other.is_a?(Concat)
        other.patterns.take_while(&method(:native_concat?))
      end

      # @!visibility private
      def native_concat(other)
        "#{self}#{other}" if native_concat?(other)
      end

      # @!visibility private
      def native_concat?(other)
        other.class == self.class and other.options == options
      end

      private :native_concat, :native_concat?
    end

    # Should not be used directly.
    # @!visibility private
    def initialize(*)
      super
      AST::Validation.validate(combined_ast) if respond_to? :expand
    end

    # @see Mustermann::Composite#operator
    # @return [Symbol] always :+
    def operator
      :+
    end

    # @see Mustermann::Pattern#===
    def ===(string)
      peek_size(string) == string.size
    end

    # @see Mustermann::Pattern#match
    def match(string)
      peeked = peek_match(string)
      peeked if peeked.to_s == string
    end

    # @see Mustermann::Pattern#params
    def params(string)
      params, size = peek_params(string)
      params if size == string.size
    end

    # @see Mustermann::Pattern#peek_size
    def peek_size(string)
      pump(string) { |p,s| p.peek_size(s) }
    end

    # @see Mustermann::Pattern#peek_match
    def peek_match(string)
      pump(string, initial: SimpleMatch.new) do |pattern, substring|
        return unless match = pattern.peek_match(substring)
        [match, match.to_s.size]
      end
    end

    # @see Mustermann::Pattern#peek_params
    def peek_params(string)
      pump(string, inject_with: :merge, with_size: true) { |p, s| p.peek_params(s) }
    end

    # (see Mustermann::Pattern#expand)
    def expand(behavior = nil, values = {})
      raise NotImplementedError, 'expanding not supported' unless respond_to? :expand
      @expander ||= Mustermann::Expander.new(self) { combined_ast }
      @expander.expand(behavior, values)
    end

    # (see Mustermann::Pattern#to_templates)
    def to_templates
      raise NotImplementedError, 'template generation not supported' unless respond_to? :to_templates
      @to_templates ||= patterns.inject(['']) { |list, pattern| list.product(pattern.to_templates).map(&:join) }.uniq
    end

    # @!visibility private
    def respond_to_special?(method)
      method = :to_ast if method.to_sym == :expand
      patterns.all? { |p| p.respond_to?(method) }
    end

    # used to generate results for various methods by scanning through an input string
    # @!visibility private
    def pump(string, inject_with: :+, initial: nil, with_size: false)
      substring = string
      results   = Array(initial)

      patterns.each do |pattern|
        result, size = yield(pattern, substring)
        return unless result
        results << result
        size    ||= result
        substring = substring[size..-1]
      end

      results = results.inject(inject_with)
      with_size ? [results, string.size - substring.size] : results
    end

    # generates one big AST from all patterns
    # will not check if patterns support AST generation
    # @!visibility private
    def combined_ast
      payload = patterns.map { |p| AST::Node[:group].new(p.to_ast.payload) }
      AST::Node[:root].new(payload)
    end

    private :combined_ast, :pump
  end
end
