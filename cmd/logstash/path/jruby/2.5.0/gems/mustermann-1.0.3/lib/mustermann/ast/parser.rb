# frozen_string_literal: true
require 'mustermann/ast/node'
require 'forwardable'
require 'strscan'

module Mustermann
  # @see Mustermann::AST::Pattern
  module AST
    # Simple, StringScanner based parser.
    # @!visibility private
    class Parser
      # @param [String] string to be parsed
      # @return [Mustermann::AST::Node] parse tree for string
      # @!visibility private
      def self.parse(string, **options)
        new(**options).parse(string)
      end

      # Defines another grammar rule for first character.
      #
      # @see Mustermann::Rails
      # @see Mustermann::Sinatra
      # @see Mustermann::Template
      # @!visibility private
      def self.on(*chars, &block)
        chars.each do |char|
          define_method("read %p" % char, &block)
        end
      end

      # Defines another grammar rule for a suffix.
      #
      # @see Mustermann::Sinatra
      # @!visibility private
      def self.suffix(pattern = /./, after: :node, &block)
        @suffix ||= []
        @suffix << [pattern, after, block] if block
        @suffix
      end

      # @!visibility private
      attr_reader :buffer, :string, :pattern

      extend Forwardable
      def_delegators :buffer, :eos?, :getch, :pos

      # @!visibility private
      def initialize(pattern: nil, **options)
        @pattern = pattern
      end

      # @param [String] string to be parsed
      # @return [Mustermann::AST::Node] parse tree for string
      # @!visibility private
      def parse(string)
        @string = string
        @buffer = ::StringScanner.new(string)
        node(:root, string) { read unless eos? }
      end

      # @example
      #   node(:char, 'x').compile =~ 'x' # => true
      #
      # @param [Symbol] type node type
      # @return [Mustermann::AST::Node]
      # @!visibility private
      def node(type, *args, &block)
        type  = Node[type] unless type.respond_to? :new
        start = pos
        node  = block ? type.parse(*args, &block) : type.new(*args)
        min_size(start, pos, node)
      end

      # Create a node for a character we don't have an explicit rule for.
      #
      # @param [String] char the character
      # @return [Mustermann::AST::Node] the node
      # @!visibility private
      def default_node(char)
        char == ?/ ? node(:separator, char) : node(:char, char)
      end

      # Reads the next element from the buffer.
      # @return [Mustermann::AST::Node] next element
      # @!visibility private
      def read
        start  = pos
        char   = getch
        method = "read %p" % char
        element= respond_to?(method) ? send(method, char) : default_node(char)
        min_size(start, pos, element)
        read_suffix(element)
      end

      # sets start on node to start if it's not set to a lower value.
      # sets stop on node to stop if it's not set to a higher value.
      # @return [Mustermann::AST::Node] the node passed as third argument
      # @!visibility private
      def min_size(start, stop, node)
        stop  ||= start
        start ||= stop
        node.start = start unless node.start and node.start < start
        node.stop  = stop  unless node.stop  and node.stop  > stop
        node
      end

      # Checks for a potential suffix on the buffer.
      # @param [Mustermann::AST::Node] element node without suffix
      # @return [Mustermann::AST::Node] node with suffix
      # @!visibility private
      def read_suffix(element)
        self.class.suffix.inject(element) do |ele, (regexp, after, callback)|
          next ele unless ele.is_a?(after) and payload = scan(regexp)
          content = instance_exec(payload, ele, &callback)
          min_size(element.start, pos, content)
        end
      end

      # Wrapper around {StringScanner#scan} that turns strings into escaped
      # regular expressions and returns a MatchData if the regexp has any
      # named captures.
      #
      # @param [Regexp, String] regexp
      # @see StringScanner#scan
      # @return [String, MatchData, nil]
      # @!visibility private
      def scan(regexp)
        regexp = Regexp.new(Regexp.escape(regexp)) unless regexp.is_a? Regexp
        string = buffer.scan(regexp)
        regexp.names.any? ? regexp.match(string) : string
      end

      # Asserts a regular expression matches what's next on the buffer.
      # Will return corresponding MatchData if regexp includes named captures.
      #
      # @param [Regexp] regexp expected to match
      # @return [String, MatchData] the match
      # @raise [Mustermann::ParseError] if expectation wasn't met
      # @!visibility private
      def expect(regexp, char: nil, **options)
        scan(regexp) || unexpected(char, **options)
      end

      # Allows to read a string inside brackets. It does not expect the string
      # to start with an opening bracket.
      #
      # @example
      #   buffer.string = "fo<o>>ba<r>"
      #   read_brackets(?<, ?>) # => "fo<o>"
      #   buffer.rest # => "ba<r>"
      #
      # @!visibility private
      def read_brackets(open, close, char: nil, escape: ?\\, quote: false, **options)
        result = String.new
        escape = false if escape.nil?
        while current = getch
          case current
          when close  then return result
          when open   then result << open   << read_brackets(open, close) << close
          when escape then result << escape << getch
          else result << current
          end
        end
        unexpected(char, **options)
      end


      # Reads an argument string of the format arg1,args2,key:value
      #
      # @!visibility private
      def read_args(key_separator, close, separator: ?,, symbol_keys: true, **options)
        list, map = [], {}
        while buffer.peek(1) != close
          scan(separator)
          entries = read_list(close, separator, separator: key_separator, **options)
          case entries.size
          when 1 then list += entries
          when 2 then map[symbol_keys ? entries.first.to_sym : entries.first] = entries.last
          else        unexpected(key_separator)
          end
          buffer.pos -= 1
        end
        expect(close)
        [list, map]
      end

      # Reads a separated list with the ability to quote, escape and add spaces.
      #
      # @!visibility private
      def read_list(*close, separator: ?,, escape: ?\\, quotes: [?", ?'], ignore: " ", **options)
        result = []
        while current = getch
          element = result.empty? ? result : result.last
          case current
          when *close    then return result
          when ignore    then nil # do nothing
          when separator then result  << String.new
          when escape    then element << getch
          when *quotes   then element << read_escaped(current, escape: escape)
          else element << current
          end
        end
        unexpected(current, **options)
      end

      # Read a string until a terminating character, ignoring escaped versions of said character.
      #
      # @!visibility private
      def read_escaped(close, escape: ?\\, **options)
        result = String.new
        while current = getch
          case current
          when close  then return result
          when escape then result << getch
          else result << current
          end
        end
        unexpected(current, **options)
      end

      # Helper for raising an exception for an unexpected character.
      # Will read character from buffer if buffer is passed in.
      #
      # @param [String, nil] char the unexpected character
      # @raise [Mustermann::ParseError, Exception]
      # @!visibility private
      def unexpected(char = nil, exception: ParseError)
        char ||= getch
        char = "space" if char == " "
        raise exception, "unexpected #{char || "end of string"} while parsing #{string.inspect}"
      end
    end
  end
end
