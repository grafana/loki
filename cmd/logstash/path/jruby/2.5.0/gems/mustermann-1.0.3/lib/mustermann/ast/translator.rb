# frozen_string_literal: true
require 'mustermann/ast/node'
require 'mustermann/error'
require 'delegate'

module Mustermann
  module AST
    # Implements translator pattern
    #
    # @abstract
    # @!visibility private
    class Translator
      # Encapsulates a single node translation
      # @!visibility private
      class NodeTranslator < DelegateClass(Node)
        # @param [Array<Symbol, Class>] types list of types to register for.
        # @!visibility private
        def self.register(*types)
          types.each do |type|
            type = Node.constant_name(type) if type.is_a? Symbol
            translator.dispatch_table[type.to_s] = self
          end
        end

        # @param node [Mustermann::AST::Node, Object]
        # @param translator [Mustermann::AST::Translator]
        #
        # @!visibility private
        def initialize(node, translator)
          @translator = translator
          super(node)
        end

        # @!visibility private
        attr_reader :translator

        # shorthand for translating a nested object
        # @!visibility private
        def t(*args, &block)
          return translator unless args.any?
          translator.translate(*args, &block)
        end

        # @!visibility private
        alias_method :node, :__getobj__
      end

      # maps types to translations
      # @!visibility private
      def self.dispatch_table
        @dispatch_table ||= {}
      end

      # some magic sauce so {NodeTranslator}s know whom to talk to for {#register}
      # @!visibility private
      def self.inherited(subclass)
        node_translator = Class.new(NodeTranslator)
        node_translator.define_singleton_method(:translator) { subclass }
        subclass.const_set(:NodeTranslator, node_translator)
        super
      end

      # DSL-ish method for specifying the exception class to use.
      # @!visibility private
      def self.raises(error)
        define_method(:error_class) { error }
      end

      # DSL method for defining single method translations.
      # @!visibility private
      def self.translate(*types, &block)
        Class.new(const_get(:NodeTranslator)) do
          register(*types)
          define_method(:translate, &block)
        end
      end

      # Enables quick creation of a translator object.
      #
      # @example
      #   require 'mustermann'
      #   require 'mustermann/ast/translator'
      #
      #   translator = Mustermann::AST::Translator.create do
      #     translate(:node)  { [type, *t(payload)].flatten.compact }
      #     translate(Array)  { map { |e| t(e) } }
      #     translate(Object) { }
      #   end
      #
      #   ast = Mustermann.new('/:name').to_ast
      #   translator.translate(ast) # => [:root, :separator, :capture]
      #
      # @!visibility private
      def self.create(&block)
        Class.new(self, &block).new
      end

      raises Mustermann::Error

      # @param [Mustermann::AST::Node, Object] node to translate
      # @return decorator encapsulating translation
      #
      # @!visibility private
      def decorator_for(node)
        factory = node.class.ancestors.inject(nil) { |d,a| d || self.class.dispatch_table[a.name] }
        raise error_class, "#{self.class}: Cannot translate #{node.class}" unless factory
        factory.new(node, self)
      end

      # Start the translation dance for a (sub)tree.
      # @!visibility private
      def translate(node, *args, &block)
        result = decorator_for(node).translate(*args, &block)
        result = result.node while result.is_a? NodeTranslator
        result
      end

      # @return [String] escaped character
      # @!visibility private
      def escape(char, parser: URI::DEFAULT_PARSER, escape: parser.regexp[:UNSAFE], also_escape: nil)
        escape = Regexp.union(also_escape, escape) if also_escape
        char =~ escape ? parser.escape(char, Regexp.union(*escape)) : char
      end
    end
  end
end
