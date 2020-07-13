# frozen_string_literal: true
require 'mustermann'
require 'mustermann/expander'

module Mustermann
  # A mapper allows mapping one string to another based on pattern parsing and expanding.
  #
  # @example
  #   require 'mustermann/mapper'
  #   mapper = Mustermann::Mapper.new("/:foo" => "/:foo.html")
  #   mapper['/example'] # => "/example.html"
  class Mapper
    # Creates a new mapper.
    #
    # @overload initialize(**options)
    #   @param options [Hash] options The options hash
    #   @yield block for generating mappings as a hash
    #   @yieldreturn [Hash] see {#update}
    #
    #   @example
    #     require 'mustermann/mapper'
    #     Mustermann::Mapper.new(type: :rails) {{
    #       "/:foo" => ["/:foo.html", "/:foo.:format"]
    #     }}
    #
    # @overload initialize(**options)
    #   @param  options [Hash] options The options hash
    #   @yield block for generating mappings as a hash
    #   @yieldparam mapper [Mustermann::Mapper] the mapper instance
    #
    #   @example
    #     require 'mustermann/mapper'
    #     Mustermann::Mapper.new(type: :rails) do |mapper|
    #       mapper["/:foo"] = ["/:foo.html", "/:foo.:format"]
    #     end
    #
    # @overload initialize(map = {}, **options)
    #   @param map [Hash] see {#update}
    #   @param [Hash] options The options hash
    #
    #   @example map before options
    #     require 'mustermann/mapper'
    #     Mustermann::Mapper.new("/:foo" => "/:foo.html", type: :rails)
    #
    #   @example map after options
    #     require 'mustermann/mapper'
    #     Mustermann::Mapper.new(type: :rails, "/:foo" => "/:foo.html")
    def initialize(map = {}, additional_values: :ignore, **options, &block)
      @map               = []
      @options           = options
      @additional_values = additional_values
      block.arity == 0 ? update(yield) : yield(self) if block
      update(map) if map
    end

    # Add multiple mappings.
    #
    # @param map [Hash{String, Pattern: String, Pattern, Arry<String, Pattern>, Expander}] the mapping
    def update(map)
      map.to_h.each_pair do |input, output|
        input  = Mustermann.new(input, **@options)
        output = Expander.new(*output, additional_values: @additional_values, **@options) unless output.is_a? Expander
        @map << [input, output]
      end
    end

    # @return [Hash{Patttern: Expander}] Hash version of the mapper.
    def to_h
      Hash[@map]
    end

    # Convert a string according to mappings. You can pass in additional params.
    #
    # @example mapping with and without additional parameters
    #   mapper = Mustermann::Mapper.new("/:example" => "(/:prefix)?/:example.html")
    #
    def convert(input, values = {})
      @map.inject(input) do |current, (pattern, expander)|
        params = pattern.params(current)
        params &&= Hash[values.merge(params).map { |k,v| [k.to_s, v] }]
        expander.expandable?(params) ? expander.expand(params) : current
      end
    end

    # Add a single mapping.
    #
    # @param key [String, Pattern] format of the input string
    # @param value [String, Pattern, Arry<String, Pattern>, Expander] format of the output string
    def []=(key, value)
      update key => value
    end

    alias_method :[], :convert
  end
end
