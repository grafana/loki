module Mustermann
  # @see Mustermann::AST::Pattern
  module AST
    # @!visibility private
    class Node
      # @!visibility private
      attr_accessor :payload, :start, :stop

      # @!visibility private
      # @param [Symbol] name of the node
      # @return [Class] factory for the node
      def self.[](name)
        @names       ||= {}
        @names[name] ||= begin
          const_name = constant_name(name)
          Object.const_get(const_name) if Object.const_defined?(const_name)
        end
      end

      # Turns a class name into a node identifier.
      # @!visibility private
      def self.type
        name[/[^:]+$/].split(/(?<=.)(?=[A-Z])/).map(&:downcase).join(?_).to_sym
      end

      # @!visibility private
      # @param [Symbol] name of the node
      # @return [String] qualified name of factory for the node
      def self.constant_name(name)
        return self.name if name.to_sym == :node
        name = name.to_s.split(?_).map(&:capitalize).join
        "#{self.name}::#{name}"
      end

      # Helper for creating a new instance and calling #parse on it.
      # @return [Mustermann::AST::Node]
      # @!visibility private
      def self.parse(*args, &block)
        new(*args).tap { |n| n.parse(&block) }
      end

      # @!visibility private
      def initialize(payload = nil, **options)
        options.each { |key, value| public_send("#{key}=", value) }
        self.payload = payload
      end

      # @!visibility private
      def is_a?(type)
        type = Node[type] if type.is_a? Symbol
        super(type)
      end

      # Double dispatch helper for reading from the buffer into the payload.
      # @!visibility private
      def parse
        self.payload ||= []
        while element = yield
          payload << element
        end
      end

      # Loop through all nodes that don't have child nodes.
      # @!visibility private
      def each_leaf(&block)
        return enum_for(__method__) unless block_given?
        called = false
        Array(payload).each do |entry|
          next unless entry.respond_to? :each_leaf
          entry.each_leaf(&block)
          called = true
        end
        yield(self) unless called
      end

      # @return [Integer] length of the substring
      # @!visibility private
      def length
        stop - start if start and stop
      end

      # @return [Integer] minimum size for a node
      # @!visibility private
      def min_size
        0
      end

      # Turns a class name into a node identifier.
      # @!visibility private
      def type
        self.class.type
      end

      # @!visibility private
      class Capture < Node
        # @see Mustermann::AST::Compiler::Capture#default
        # @!visibility private
        attr_accessor :constraint

        # @see Mustermann::AST::Compiler::Capture#qualified
        # @!visibility private
        attr_accessor :qualifier

        # @see Mustermann::AST::Pattern#map_param
        # @!visibility private
        attr_accessor :convert

        # @see Mustermann::AST::Node#parse
        # @!visibility private
        def parse
          self.payload ||= ""
          super
        end

        # @!visibility private
        alias_method :name, :payload
      end

      # @!visibility private
      class Char < Node
        # @return [Integer] minimum size for a node
        # @!visibility private
        def min_size
          1
        end
      end

      # AST node for template expressions.
      # @!visibility private
      class Expression < Node
        # @!visibility private
        attr_accessor :operator
      end

      # @!visibility private
      class Composition < Node
        # @!visibility private
        def initialize(payload = nil, **options)
          super(Array(payload), **options)
        end
      end

      # @!visibility private
      class Group < Composition
      end

      # @!visibility private
      class Union < Composition
      end

      # @!visibility private
      class Optional < Node
      end

      # @!visibility private
      class Or < Node
      end

      # @!visibility private
      class Root < Node
        # @!visibility private
        attr_accessor :pattern

        # Will trigger transform.
        #
        # @see Mustermann::AST::Node.parse
        # @!visibility private
        def self.parse(string, &block)
          root         = new
          root.pattern = string
          root.parse(&block)
          root
        end
      end

      # @!visibility private
      class Separator < Node
        # @return [Integer] minimum size for a node
        # @!visibility private
        def min_size
          1
        end
      end

      # @!visibility private
      class Splat < Capture
        # @see Mustermann::AST::Node::Capture#name
        # @!visibility private
        def name
          "splat"
        end
      end

      # @!visibility private
      class NamedSplat < Splat
        # @see Mustermann::AST::Node::Capture#name
        # @!visibility private
        alias_method :name, :payload
      end

      # AST node for template variables.
      # @!visibility private
      class Variable < Capture
        # @!visibility private
        attr_accessor :prefix, :explode
      end

      # @!visibility private
      class WithLookAhead < Node
        # @!visibility private
        attr_accessor :head, :at_end

        # @!visibility private
        def initialize(payload, at_end, **options)
          super(**options)
          self.head, *self.payload = Array(payload)
          self.at_end = at_end
        end
      end
    end
  end
end
