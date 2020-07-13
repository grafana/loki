# frozen_string_literal: true
require 'mustermann/ast/translator'

module Mustermann
  # @see Mustermann::AST::Pattern
  module AST
    # Regexp compilation logic.
    # @!visibility private
    class Compiler < Translator
      raises CompileError

      # Trivial compilations
      translate(Array)      { |**o| map { |e| t(e, **o) }.join  }
      translate(:node)      { |**o| t(payload, **o)             }
      translate(:separator) { |**o| Regexp.escape(payload)      }
      translate(:optional)  { |**o| "(?:%s)?" % t(payload, **o) }
      translate(:char)      { |**o| t.encoded(payload, **o)     }

      translate :union do |**options|
        "(?:%s)" % payload.map { |e| "(?:%s)" % t(e, **options) }.join(?|)
      end

      translate :expression do |greedy: true, **options|
        t(payload, allow_reserved: operator.allow_reserved, greedy: greedy && !operator.allow_reserved,
          parametric: operator.parametric, separator: operator.separator, **options)
      end

      translate :with_look_ahead do |**options|
        lookahead = each_leaf.inject("") do |ahead, element|
          ahead + t(element, skip_optional: true, lookahead: ahead, greedy: false, no_captures: true, **options).to_s
        end
        lookahead << (at_end ? '$' : '/')
        t(head, lookahead: lookahead, **options) + t(payload, **options)
      end

      # Capture compilation is complex. :(
      # @!visibility private
      class Capture < NodeTranslator
        register :capture

        # @!visibility private
        def translate(**options)
          return pattern(options) if options[:no_captures]
          "(?<#{name}>#{translate(no_captures: true, **options)})"
        end

        # @return [String] regexp without the named capture
        # @!visibility private
        def pattern(capture: nil, **options)
          case capture
          when Symbol then from_symbol(capture, **options)
          when Array  then from_array(capture, **options)
          when Hash   then from_hash(capture, **options)
          when String then from_string(capture, **options)
          when nil    then from_nil(**options)
          else capture
          end
        end

        private
          def qualified(string, greedy: true, **options)        "#{string}#{qualifier || "+#{?? unless greedy}"}"                    end
          def with_lookahead(string, lookahead: nil, **options)  lookahead ? "(?:(?!#{lookahead})#{string})" : string                end
          def from_hash(hash,     **options)                     pattern(capture: hash[name.to_sym], **options)                      end
          def from_array(array,   **options)                     Regexp.union(*array.map { |e| pattern(capture: e, **options) })     end
          def from_symbol(symbol, **options)                     qualified(with_lookahead("[[:#{symbol}:]]", **options), **options)  end
          def from_string(string, **options)                     Regexp.new(string.chars.map { |c| t.encoded(c, **options) }.join)   end
          def from_nil(**options)                                qualified(with_lookahead(default(**options), **options), **options) end
          def default(**options)                                 constraint || "[^/\\?#]"                                            end
      end

      # @!visibility private
      class Splat < Capture
        register :splat, :named_splat
        # splats are always non-greedy
        # @!visibility private
        def pattern(**options)
          constraint || ".*?"
        end
      end

      # @!visibility private
      class Variable < Capture
        register :variable

        # @!visibility private
        def translate(**options)
          return super(**options) if explode or not options[:parametric]
          # Remove this line after fixing broken compatibility between 2.1 and 2.2
          options.delete(:parametric) if options.has_key?(:parametric)
          parametric super(parametric: false, **options)
        end

        # @!visibility private
        def pattern(parametric: false, separator: nil, **options)
          register_param(parametric: parametric, separator: separator, **options)
          pattern = super(**options)
          pattern = parametric(pattern) if parametric
          pattern = "#{pattern}(?:#{Regexp.escape(separator)}#{pattern})*" if explode and separator
          pattern
        end

        # @!visibility private
        def parametric(string)
          "#{Regexp.escape(name)}(?:=#{string})?"
        end

        # @!visibility private
        def qualified(string, **options)
          prefix ? "#{string}{1,#{prefix}}" : super(string, **options)
        end

        # @!visibility private
        def default(allow_reserved: false, **options)
          allow_reserved ? '[\w\-\.~%\:/\?#\[\]@\!\$\&\'\(\)\*\+,;=]' : '[\w\-\.~%]'
        end

        # @!visibility private
        def register_param(parametric: false, split_params: nil, separator: nil, **options)
          return unless explode and split_params
          split_params[name] = { separator: separator, parametric: parametric }
        end
      end

      # @return [String] Regular expression for matching the given character in all representations
      # @!visibility private
      def encoded(char, uri_decode: true, space_matches_plus: true, **options)
        return Regexp.escape(char) unless uri_decode
        encoded = escape(char, escape: /./)
        list    = [escape(char), encoded.downcase, encoded.upcase].uniq.map { |c| Regexp.escape(c) }
        if char == " "
          list << encoded('+') if space_matches_plus
          list << " "
        end
        "(?:%s)" % list.join("|")
      end

      # Compiles an AST to a regular expression.
      # @param [Mustermann::AST::Node] ast the tree
      # @return [Regexp] corresponding regular expression.
      #
      # @!visibility private
      def self.compile(ast, **options)
        new.compile(ast, **options)
      end

      # Compiles an AST to a regular expression.
      # @param [Mustermann::AST::Node] ast the tree
      # @return [Regexp] corresponding regular expression.
      #
      # @!visibility private
      def compile(ast, except: nil, **options)
        except &&= "(?!#{translate(except, no_captures: true, **options)}\\Z)"
        Regexp.new("#{except}#{translate(ast, **options)}")
      end
    end

    private_constant :Compiler
  end
end
