# frozen_string_literal: true
require 'mustermann/error'
require 'mustermann/simple_match'
require 'mustermann/equality_map'
require 'uri'

module Mustermann
  # Superclass for all pattern implementations.
  # @abstract
  class Pattern
    include Mustermann
    @@uri ||= URI::Parser.new

    # List of supported options.
    #
    # @overload supported_options
    #   @return [Array<Symbol>] list of supported options
    # @overload supported_options(*list)
    #   Adds options to the list.
    #
    #   @api private
    #   @param [Symbol] *list adds options to the list of supported options
    #   @return [Array<Symbol>] list of supported options
    def self.supported_options(*list)
      @supported_options ||= []
      options = @supported_options.concat(list)
      options += superclass.supported_options if self < Pattern
      options
    end

    # Registers the pattern with Mustermann.
    # @see Mustermann.register
    # @!visibility private
    def self.register(*names)
      names.each { |name| Mustermann.register(name, self) }
    end

    # @param [Symbol] option The option to check.
    # @return [Boolean] Whether or not option is supported.
    def self.supported?(option, **options)
      supported_options.include? option
    end

    # @overload new(string, **options)
    # @param (see #initialize)
    # @raise (see #initialize)
    # @raise [ArgumentError] if some option is not supported
    # @return [Mustermann::Pattern] a new instance of Mustermann::Pattern
    # @see #initialize
    def self.new(string, ignore_unknown_options: false, **options)
      if ignore_unknown_options
        options = options.select { |key, value| supported?(key, **options) if key != :ignore_unknown_options }
      else
        unsupported = options.keys.detect { |key| not supported?(key, **options) }
        raise ArgumentError, "unsupported option %p for %p" % [unsupported, self] if unsupported
      end

      @map ||= EqualityMap.new
      @map.fetch([string, options]) { super(string, options) { options } }
    end

    supported_options :uri_decode, :ignore_unknown_options
    attr_reader :uri_decode

    # options hash passed to new (with unsupported options removed)
    # @!visibility private
    attr_reader :options

    # @overload initialize(string, **options)
    # @param [String] string the string representation of the pattern
    # @param [Hash] options options for fine-tuning the pattern behavior
    # @raise [Mustermann::Error] if the pattern can't be generated from the string
    # @see file:README.md#Types_and_Options "Types and Options" in the README
    # @see Mustermann.new
    def initialize(string, uri_decode: true, **options)
      @uri_decode = uri_decode
      @string     = string.to_s.dup
      @options    = yield.freeze if block_given?
    end

    # @return [String] the string representation of the pattern
    def to_s
      @string.dup
    end

    # @param [String] string The string to match against
    # @return [MatchData, Mustermann::SimpleMatch, nil] MatchData or similar object if the pattern matches.
    # @see http://ruby-doc.org/core-2.0/Regexp.html#method-i-match Regexp#match
    # @see http://ruby-doc.org/core-2.0/MatchData.html MatchData
    # @see Mustermann::SimpleMatch
    def match(string)
      SimpleMatch.new(string) if self === string
    end

    # @param [String] string The string to match against
    # @return [Integer, nil] nil if pattern does not match the string, zero if it does.
    # @see http://ruby-doc.org/core-2.0/Regexp.html#method-i-3D-7E Regexp#=~
    def =~(string)
      0 if self === string
    end

    # @param [String] string The string to match against
    # @return [Boolean] Whether or not the pattern matches the given string
    # @note Needs to be overridden by subclass.
    # @see http://ruby-doc.org/core-2.0/Regexp.html#method-i-3D-3D-3D Regexp#===
    def ===(string)
      raise NotImplementedError, 'subclass responsibility'
    end

    # Used by Ruby internally for hashing.
    # @return [Integer] same has value for patterns that are equal
    def hash
      self.class.hash | @string.hash | options.hash
    end

    # Two patterns are considered equal if they are of the same type, have the same pattern string
    # and the same options.
    # @return [true, false]
    def ==(other)
      other.class == self.class and other.to_s == @string and other.options == options
    end

    # Two patterns are considered equal if they are of the same type, have the same pattern string
    # and the same options.
    # @return [true, false]
    def eql?(other)
      other.class.eql?(self.class) and other.to_s.eql?(@string) and other.options.eql?(options)
    end

    # Tries to match the pattern against the beginning of the string (as opposed to the full string).
    # Will return the count of the matching characters if it matches.
    #
    # @example
    #   pattern = Mustermann.new('/:name')
    #   pattern.size("/Frank/Sinatra") # => 6
    #
    # @param [String] string The string to match against
    # @return [Integer, nil] the number of characters that match
    def peek_size(string)
      # this is a very naive, unperformant implementation
      string.size.downto(0).detect { |s| self === string[0, s] }
    end

    # Tries to match the pattern against the beginning of the string (as opposed to the full string).
    # Will return the substring if it matches.
    #
    # @example
    #   pattern = Mustermann.new('/:name')
    #   pattern.peek("/Frank/Sinatra") # => "/Frank"
    #
    # @param [String] string The string to match against
    # @return [String, nil] matched subsctring
    def peek(string)
      size = peek_size(string)
      string[0, size] if size
    end

    # Tries to match the pattern against the beginning of the string (as opposed to the full string).
    # Will return a MatchData or similar instance for the matched substring.
    #
    # @example
    #   pattern = Mustermann.new('/:name')
    #   pattern.peek("/Frank/Sinatra") # => #<MatchData "/Frank" name:"Frank">
    #
    # @param [String] string The string to match against
    # @return [MatchData, Mustermann::SimpleMatch, nil] MatchData or similar object if the pattern matches.
    # @see #peek_params
    def peek_match(string)
      matched = peek(string)
      match(matched) if matched
    end

    # Tries to match the pattern against the beginning of the string (as opposed to the full string).
    # Will return a two element Array with the params parsed from the substring as first entry and the length of
    # the substring as second.
    #
    # @example
    #   pattern   = Mustermann.new('/:name')
    #   params, _ = pattern.peek_params("/Frank/Sinatra")
    #
    #   puts "Hello, #{params['name']}!" # Hello, Frank!
    #
    # @param [String] string The string to match against
    # @return [Array<Hash, Integer>, nil] Array with params hash and length of substing if matched, nil otherwise
    def peek_params(string)
      match = peek_match(string)
      [params(captures: match), match.to_s.size] if match
    end

    # @return [Hash{String: Array<Integer>}] capture names mapped to capture index.
    # @see http://ruby-doc.org/core-2.0/Regexp.html#method-i-named_captures Regexp#named_captures
    def named_captures
      {}
    end

    # @return [Array<String>] capture names.
    # @see http://ruby-doc.org/core-2.0/Regexp.html#method-i-names Regexp#names
    def names
      []
    end

    # @param [String] string the string to match against
    # @return [Hash{String: String, Array<String>}, nil] Sinatra style params if pattern matches.
    def params(string = nil, captures: nil, offset: 0)
      return unless captures ||= match(string)
      params   = named_captures.map do |name, positions|
        values = positions.map { |pos| map_param(name, captures[pos + offset]) }.flatten
        values = values.first if values.size < 2 and not always_array? name
        [name, values]
      end

      Hash[params]
    end

    # @note This method is only implemented by certain subclasses.
    #
    # @example Expanding a pattern
    #   pattern = Mustermann.new('/:name(.:ext)?')
    #   pattern.expand(name: 'hello')             # => "/hello"
    #   pattern.expand(name: 'hello', ext: 'png') # => "/hello.png"
    #
    # @example Checking if a pattern supports expanding
    #   if pattern.respond_to? :expand
    #     pattern.expand(name: "foo")
    #   else
    #     warn "does not support expanding"
    #   end
    #
    # Expanding is supported by almost all patterns (notable exceptions are {Mustermann::Shell},
    # {Mustermann::Regular} and {Mustermann::Simple}).
    #
    # Union {Mustermann::Composite} patterns (with the | operator) support expanding if all
    # patterns they are composed of also support it.
    #
    # @param (see Mustermann::Expander#expand)
    # @return [String] expanded string
    # @raise [NotImplementedError] raised if expand is not supported.
    # @raise [Mustermann::ExpandError] raised if a value is missing or unknown
    # @see Mustermann::Expander
    def expand(behavior = nil, values = {})
      raise NotImplementedError, "expanding not supported by #{self.class}"
    end

    # @note This method is only implemented by certain subclasses.
    #
    # Generates a list of URI template strings representing the pattern.
    #
    # Note that this transformation is lossy and the strings matching these
    # templates might not match the pattern (and vice versa).
    #
    # This comes in quite handy since URI templates are not made for pattern matching.
    # That way you can easily use a more precise template syntax and have it automatically
    # generate hypermedia links for you.
    #
    # @example generating templates
    #   Mustermann.new("/:name").to_templates                   # => ["/{name}"]
    #   Mustermann.new("/:foo(@:bar)?/*baz").to_templates       # => ["/{foo}@{bar}/{+baz}", "/{foo}/{+baz}"]
    #   Mustermann.new("/{name}", type: :template).to_templates # => ["/{name}"]
    #
    # @example generating templates from composite patterns
    #   pattern  = Mustermann.new('/:name')
    #   pattern |= Mustermann.new('/{name}', type: :template)
    #   pattern |= Mustermann.new('/example/*nested')
    #   pattern.to_templates # => ["/{name}", "/example/{+nested}"]
    #
    # Template generation is supported by almost all patterns (notable exceptions are
    # {Mustermann::Shell}, {Mustermann::Regular} and {Mustermann::Simple}).
    # Union {Mustermann::Composite} patterns (with the | operator) support template generation
    # if all patterns they are composed of also support it.
    #
    # @example Checking if a pattern supports expanding
    #   if pattern.respond_to? :to_templates
    #     pattern.to_templates
    #   else
    #     warn "does not support template generation"
    #   end
    #
    # @return [Array<String>] list of URI templates
    def to_templates
      raise NotImplementedError, "template generation not supported by #{self.class}"
    end

    # @overload |(other)
    #   Creates a pattern that matches any string matching either one of the patterns.
    #   If a string is supplied, it is treated as an identity pattern.
    #
    #   @example
    #     pattern = Mustermann.new('/foo/:name') | Mustermann.new('/:first/:second')
    #     pattern === '/foo/bar' # => true
    #     pattern === '/fox/bar' # => true
    #     pattern === '/foo'     # => false
    #
    # @overload &(other)
    #   Creates a pattern that matches any string matching both of the patterns.
    #   If a string is supplied, it is treated as an identity pattern.
    #
    #   @example
    #     pattern = Mustermann.new('/foo/:name') & Mustermann.new('/:first/:second')
    #     pattern === '/foo/bar' # => true
    #     pattern === '/fox/bar' # => false
    #     pattern === '/foo'     # => false
    #
    # @overload ^(other)
    #   Creates a pattern that matches any string matching exactly one of the patterns.
    #   If a string is supplied, it is treated as an identity pattern.
    #
    #   @example
    #     pattern = Mustermann.new('/foo/:name') ^ Mustermann.new('/:first/:second')
    #     pattern === '/foo/bar' # => false
    #     pattern === '/fox/bar' # => true
    #     pattern === '/foo'     # => false
    #
    # @param [Mustermann::Pattern, String] other the other pattern
    # @return [Mustermann::Pattern] a composite pattern
    def |(other)
      Mustermann::Composite.new(self, other, operator: __callee__, type: :identity)
    end

    alias_method :&, :|
    alias_method :^, :|

    # @example
    #   require 'mustermann'
    #   prefix = Mustermann.new("/:prefix")
    #   about  = prefix + "/about"
    #   about.params("/main/about") # => {"prefix" => "main"}
    #
    # Creates a concatenated pattern by combingin self with the other pattern supplied.
    # Patterns of different types can be mixed. The availability of `to_templates` and
    # `expand` depends on the patterns being concatenated.
    #
    # String input is treated as identity pattern.
    #
    # @param [Mustermann::Pattern, String] other pattern to be appended
    # @return [Mustermann::Pattern] concatenated pattern
    def +(other)
       Concat.new(self, other, type: :identity)
    end

    # @example
    #   pattern = Mustermann.new('/:a/:b')
    #   strings = ["foo/bar", "/foo/bar", "/foo/bar/"]
    #   strings.detect(&pattern) # => "/foo/bar"
    #
    # @return [Proc] proc wrapping {#===}
    def to_proc
      @to_proc ||= method(:===).to_proc
    end

    # @!visibility private
    # @return [Boolean]
    # @see Object#respond_to?
    def respond_to?(method, *args)
      return super unless %i[expand to_templates].include? method
      respond_to_special?(method)
    end

    # @!visibility private
    # @return [Boolean]
    # @see #respond_to?
    def respond_to_special?(method)
      method(method).owner != Mustermann::Pattern
    end

    # @!visibility private
    def inspect
      "#<%p:%p>" % [self.class, @string]
    end

    # @!visibility private
    def simple_inspect
      type = self.class.name[/[^:]+$/].downcase
      "%s:%p" % [type, @string]
    end

    # @!visibility private
    def map_param(key, value)
      unescape(value, true)
    end

    # @!visibility private
    def unescape(string, decode = uri_decode)
      return string unless decode and string
      @@uri.unescape(string)
    end

    # @!visibility private
    ALWAYS_ARRAY = %w[splat captures]

    # @!visibility private
    def always_array?(key)
      ALWAYS_ARRAY.include? key
    end

    private :unescape, :map_param, :respond_to_special?
    private_constant :ALWAYS_ARRAY
  end
end
