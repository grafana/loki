# frozen_string_literal: true
require 'mustermann/pattern'
require 'mustermann/composite'
require 'mustermann/concat'
require 'thread'

# Namespace and main entry point for the Mustermann library.
#
# Under normal circumstances the only external API entry point you should be using is {Mustermann.new}.
module Mustermann
  # Type to use if no type is given.
  # @api private
  DEFAULT_TYPE = :sinatra

  # Creates a new pattern based on input.
  #
  # * From {Mustermann::Pattern}: returns given pattern.
  # * From String: creates a pattern from the string, depending on type option (defaults to {Mustermann::Sinatra})
  # * From Regexp: creates a {Mustermann::Regular} pattern.
  # * From Symbol: creates a {Mustermann::Sinatra} pattern with a single named capture named after the input.
  # * From an Array or multiple inputs: creates a new pattern from each element, combines them to a {Mustermann::Composite}.
  # * From anything else: Will try to call to_pattern on it or raise a TypeError.
  #
  # Note that if the input is a {Mustermann::Pattern}, Regexp or Symbol, the type option is ignored and if to_pattern is
  # called on the object, the type will be handed on but might be ignored by the input object.
  #
  # If you want to enforce the pattern type, you should create them via their expected class.
  #
  # @example creating patterns
  #   require 'mustermann'
  #
  #   Mustermann.new("/:name")                    # => #<Mustermann::Sinatra:"/example">
  #   Mustermann.new("/{name}", type: :template)  # => #<Mustermann::Template:"/{name}">
  #   Mustermann.new(/.*/)                        # => #<Mustermann::Regular:".*">
  #   Mustermann.new(:name, capture: :word)       # => #<Mustermann::Sinatra:":name">
  #   Mustermann.new("/", "/*.jpg", type: :shell) # => #<Mustermann::Composite:(shell:"/" | shell:"/*.jpg")>
  #
  # @example using custom #to_pattern
  #   require 'mustermann'
  #
  #   class MyObject
  #     def to_pattern(**options)
  #       Mustermann.new("/:name", **options)
  #     end
  #   end
  #
  #   Mustermann.new(MyObject.new, type: :rails) # => #<Mustermann::Rails:"/:name">
  #
  # @example enforcing type
  #   require 'mustermann/sinatra'
  #
  #   Mustermann::Sinatra.new("/:name")
  #
  # @param [String, Pattern, Regexp, Symbol, #to_pattern, Array<String, Pattern, Regexp, Symbol, #to_pattern>]
  #   input The representation of the pattern
  # @param [Hash] options The options hash
  # @return [Mustermann::Pattern] pattern corresponding to string.
  # @raise (see [])
  # @raise (see Mustermann::Pattern.new)
  # @raise [TypeError] if the passed object cannot be converted to a pattern
  # @see file:README.md#Types_and_Options "Types and Options" in the README
  def self.new(*input, type: DEFAULT_TYPE, operator: :|, **options)
    type ||= DEFAULT_TYPE
    input  = input.first if input.size < 2
    case input
    when Pattern then input
    when Regexp  then self[:regexp].new(input, **options)
    when String  then self[type].new(input, **options)
    when Symbol  then self[:sinatra].new(input.inspect, **options)
    when Array   then input.map { |i| new(i, type: type, **options) }.inject(operator)
    else
      pattern = input.to_pattern(type: type, **options) if input.respond_to? :to_pattern
      raise TypeError, "#{input.class} can't be coerced into Mustermann::Pattern" if pattern.nil?
      pattern
    end
  end

  @mutex ||= Mutex.new
  @types ||= {}

  # Maps a type to its factory.
  #
  # @example
  #   Mustermann[:sinatra] # => Mustermann::Sinatra
  #
  # @param [Symbol] name a pattern type identifier
  # @raise [ArgumentError] if the type is not supported
  # @return [Class, #new] pattern factory
  def self.[](name)
    return name if name.respond_to? :new
    @types.fetch(normalized = normalized_type(name)) do
      @mutex.synchronize do
        error = try_require "mustermann/#{normalized}"
        @types.fetch(normalized) { raise ArgumentError, "unsupported type %p#{" (#{error.message})" if error}" % name }
      end
    end
  end

  # @return [LoadError, nil]
  # @!visibility private
  def self.try_require(path)
    require(path)
    nil
  rescue LoadError => error
    raise(error) unless error.path == path
    error
  end

  # @!visibility private
  def self.register(name, type)
    @types[normalized_type(name)] = type
  end

  # @!visibility private
  def self.normalized_type(type)
    type.to_s.gsub('-', '_').downcase
  end

  # @!visibility private
  def self.extend_object(object)
    return super unless defined? ::Sinatra::Base and object.is_a? Class and object < ::Sinatra::Base
    require 'mustermann/extension'
    object.register Extension
  end
end

# :nocov:
begin
  require 'mustermann/visualizer' if defined?(Pry) or defined?(IRB)
rescue LoadError => error
  raise error unless error.path == 'mustermann/visualizer'
  $stderr.puts(error.message) if caller_locations[1].absolute_path =~ %r{/lib/pry/|/irb/|^\((?:irb|pry)\)$}
end
# :nocov:
