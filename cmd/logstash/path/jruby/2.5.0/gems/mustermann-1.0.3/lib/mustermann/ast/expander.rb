# frozen_string_literal: true
require 'mustermann/ast/translator'
require 'mustermann/ast/compiler'

module Mustermann
  module AST
    # Looks at an AST, remembers the important bits of information to do an
    # ultra fast expansion.
    #
    # @!visibility private
    class Expander < Translator
      raises ExpandError

      translate Array do |*args|
        inject(t.pattern) do |pattern, element|
          t.add_to(pattern, t(element, *args))
        end
      end

      translate :capture do |**options|
        t.for_capture(node, **options)
      end

      translate :named_splat, :splat do
        t.pattern + t.for_capture(node)
      end

      translate :expression do
        t(payload, allow_reserved: operator.allow_reserved)
      end

      translate :root, :group do
        t(payload)
      end

      translate :char do
        t.pattern(t.escape(payload, also_escape: /[\/\?#\&\=%]/).gsub(?%, "%%"))
      end

      translate :separator do
        t.pattern(payload.gsub(?%, "%%"))
      end

      translate :with_look_ahead do
        t.add_to(t(head), t(payload))
      end

      translate :optional do
        nested = t(payload)
        nested += t.pattern unless nested.any? { |n| n.first.empty? }
        nested
      end

      translate :union do
        payload.map { |e| t(e) }.inject(:+)
      end

      # helper method for captures
      # @!visibility private
      def for_capture(node, **options)
        name = node.name.to_sym
        pattern('%s', name, name => /(?!#{pattern_for(node, **options)})./)
      end

      # maps sorted key list to sprintf patterns and filters
      # @!visibility private
      def mappings
        @mappings ||= {}
      end

      # all the known keys
      # @!visibility private
      def keys
        @keys ||= []
      end

      # add a tree for expansion
      # @!visibility private
      def add(ast)
        translate(ast).each do |keys, pattern, filter|
          self.keys.concat(keys).uniq!
          mappings[keys.sort] ||= [keys, pattern, filter]
        end
      end

      # helper method for getting a capture's pattern.
      # @!visibility private
      def pattern_for(node, **options)
        Compiler.new.decorator_for(node).pattern(**options)
      end

      # @see Mustermann::Pattern#expand
      # @!visibility private
      def expand(values)
        values = values.each_with_object({}){ |(key, value), new_hash|
          new_hash[value.instance_of?(Array) ? [key] * value.length : key] = value }
        keys, pattern, filters = mappings.fetch(values.keys.flatten.sort) { error_for(values) }
        filters.each { |key, filter| values[key] &&= escape(values[key], also_escape: filter) }
        pattern % (values[keys] || values.values_at(*keys))
      end

      # @see Mustermann::Pattern#expandable?
      # @!visibility private
      def expandable?(values)
        values = values.keys if values.respond_to? :keys
        values = values.sort if values.respond_to? :sort
        mappings.include? values
      end

      # @see Mustermann::Expander#with_rest
      # @!visibility private
      def expandable_keys(keys)
        mappings.keys.select { |k| (k - keys).empty? }.max_by(&:size) || keys
      end

      # helper method for raising an error for unexpandable values
      # @!visibility private
      def error_for(values)
        expansions = mappings.keys.map(&:inspect).join(" or ")
        raise error_class, "cannot expand with keys %p, possible expansions: %s" % [values.keys.sort, expansions]
      end

      # @see Mustermann::AST::Translator#expand
      # @!visibility private
      def escape(string, *args)
        # URI::Parser is pretty slow, let's not send every string to it, even if it's unnecessary
        string =~ /\A\w*\Z/ ? string : super
      end

      # Turns a sprintf pattern into our secret internal data structure.
      # @!visibility private
      def pattern(string = "", *keys, **filters)
        [[keys, string, filters]]
      end

      # Creates the product of two of our secret internal data structures.
      # @!visibility private
      def add_to(list, result)
        list << [[], ""] if list.empty?
        list.inject([]) { |l, (k1, p1, f1)| l + result.map { |k2, p2, f2| [k1+k2, p1+p2, **f1, **f2] } }
      end
    end
  end
end
