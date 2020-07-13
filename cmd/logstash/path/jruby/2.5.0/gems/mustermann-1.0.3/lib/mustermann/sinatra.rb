# frozen_string_literal: true
require 'mustermann'
require 'mustermann/identity'
require 'mustermann/ast/pattern'
require 'mustermann/sinatra/parser'
require 'mustermann/sinatra/safe_renderer'
require 'mustermann/sinatra/try_convert'

module Mustermann
  # Sinatra 2.0 style pattern implementation.
  #
  # @example
  #   Mustermann.new('/:foo') === '/bar' # => true
  #
  # @see Mustermann::Pattern
  # @see file:README.md#sinatra Syntax description in the README
  class Sinatra < AST::Pattern
    include Concat::Native
    register :sinatra

    # Takes a string and espaces any characters that have special meaning for Sinatra patterns.
    #
    # @example
    #   require 'mustermann/sinatra'
    #   Mustermann::Sinatra.escape("/:name") # => "/\\:name"
    #
    # @param [#to_s] string the input string
    # @return [String] the escaped string
    def self.escape(string)
      string.to_s.gsub(/[\?\(\)\*:\\\|\{\}]/) { |c| "\\#{c}" }
    end

    # Tries to convert the given input object to a Sinatra pattern with the given options, without
    # changing its parsing semantics.
    # @return [Mustermann::Sinatra, nil] the converted pattern, if possible
    # @!visibility private
    def self.try_convert(input, **options)
      TryConvert.convert(input, **options)
    end

    # Creates a pattern that matches any string matching either one of the patterns.
    # If a string is supplied, it is treated as a fully escaped Sinatra pattern.
    #
    # If the other pattern is also a Sintara pattern, it might join the two to a third
    # sinatra pattern instead of generating a composite for efficiency reasons.
    #
    # This only happens if the sinatra pattern behaves exactly the same as a composite
    # would in regards to matching, parsing, expanding and template generation.
    #
    # @example
    #   pattern = Mustermann.new('/foo/:name') | Mustermann.new('/:first/:second')
    #   pattern === '/foo/bar' # => true
    #   pattern === '/fox/bar' # => true
    #   pattern === '/foo'     # => false
    #
    # @param [Mustermann::Pattern, String] other the other pattern
    # @return [Mustermann::Pattern] a composite pattern
    # @see Mustermann::Pattern#|
    def |(other)
      return super unless converted = self.class.try_convert(other, **options)
      return super unless converted.names.empty? or names.empty?
      self.class.new(safe_string + "|" + converted.safe_string, **options)
    end

    # Generates a string represenation of the pattern that can safely be used for def interpolation
    # without changing its semantics.
    #
    # @example
    #   require 'mustermann'
    #   unsafe = Mustermann.new("/:name")
    #
    #   Mustermann.new("#{unsafe}bar").params("/foobar") # => { "namebar" => "foobar" }
    #   Mustermann.new("#{unsafe.safe_string}bar").params("/foobar") # => { "name" => "bar" }
    #
    # @return [String] string representatin of the pattern
    def safe_string
      @safe_string ||= SafeRenderer.translate(to_ast)
    end

    # @!visibility private
    def native_concat(other)
      return unless converted = self.class.try_convert(other, **options)
      safe_string + converted.safe_string
    end

    private :native_concat
  end
end
