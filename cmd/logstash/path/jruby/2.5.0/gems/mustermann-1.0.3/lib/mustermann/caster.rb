# frozen_string_literal: true
require 'delegate'

module Mustermann
  # Class for defining and running simple Hash transformations.
  #
  # @example
  #   caster = Mustermann::Caster.new
  #   caster.register(:foo) { |value| { bar: value.upcase } }
  #   caster.cast(foo: "hello", baz: "world") # => { bar: "HELLO", baz: "world" }
  #
  # @see Mustermann::Expander#cast
  #
  # @!visibility private
  class Caster < DelegateClass(Array)
    # @param (see #register)
    # @!visibility private
    def initialize(*types, &block)
      super([])
      register(*types, &block)
    end

    # @param [Array<Symbol, Regexp, #cast, #===>] types identifier for cast type (some need block)
    # @!visibility private
    def register(*types, &block)
      return if types.empty? and block.nil?
      types << Any.new(&block) if types.empty?
      types.each { |type| self << caster_for(type, &block) }
    end

    # @param [Symbol, Regexp, #cast, #===] type identifier for cast type (some need block)
    # @return [#cast] specific cast operation
    # @!visibility private
    def caster_for(type, &block)
      case type
      when Symbol, Regexp then Key.new(type, &block)
      else type.respond_to?(:cast) ? type : Value.new(type, &block)
      end
    end

    # Transforms a Hash.
    # @param [Hash] hash pre-transform Hash
    # @return [Hash] post-transform Hash
    # @!visibility private
    def cast(hash)
      return hash if empty?
      merge = {}
      hash.delete_if do |key, value|
        next unless casted = lazy.map { |e| e.cast(key, value) }.detect { |e| e }
        casted = { key => casted } unless casted.respond_to? :to_hash
        merge.update(casted.to_hash)
      end
      hash.update(merge)
    end

    # Class for block based casts that are triggered for every key/value pair.
    # @!visibility private
    class Any
      # @!visibility private
      def initialize(&block)
        @block = block
      end

      # @see Mustermann::Caster#cast
      # @!visibility private
      def cast(key, value)
        case @block.arity
        when 0 then @block.call
        when 1 then @block.call(value)
        else        @block.call(key, value)
        end
      end
    end

    # Class for block based casts that are triggered for key/value pairs with a matching value.
    # @!visibility private
    class Value < Any
      # @param [#===] type used for matching values
      # @!visibility private
      def initialize(type, &block)
        @type = type
        super(&block)
      end

      # @see Mustermann::Caster#cast
      # @!visibility private
      def cast(key, value)
        super if @type === value
      end
    end

    # Class for block based casts that are triggered for key/value pairs with a matching key.
    # @!visibility private
    class Key < Any
      # @param [#===] type used for matching keys
      # @!visibility private
      def initialize(type, &block)
        @type = type
        super(&block)
      end

      # @see Mustermann::Caster#cast
      # @!visibility private
      def cast(key, value)
        super if @type === key
      end
    end
  end
end
