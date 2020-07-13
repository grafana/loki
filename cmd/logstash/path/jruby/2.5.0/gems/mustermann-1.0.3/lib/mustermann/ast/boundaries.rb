# frozen_string_literal: true
require 'mustermann/ast/translator'

module Mustermann
  module AST
    # Make sure #start and #stop is set on every node and within its parents #start and #stop.
    # @!visibility private
    class Boundaries < Translator
      # @return [Mustermann::AST::Node] the ast passed as first argument
      # @!visibility private
      def self.set_boundaries(ast, string: nil, start: 0, stop: string.length)
        new.translate(ast, start, stop)
        ast
      end

      translate(:node) do |start, stop|
        t.set_boundaries(node, start, stop)
        t(payload, node.start, node.stop)
      end

      translate(:with_look_ahead) do |start, stop|
        t.set_boundaries(node, start, stop)
        t(head,    node.start, node.stop)
        t(payload, node.start, node.stop)
      end

      translate(Array) do |start, stop|
        each do |subnode|
          t(subnode, start, stop)
          start = subnode.stop
        end
      end

      translate(Object) { |*| node }

      # Checks that a node is within the given boundaries.
      # @!visibility private
      def set_boundaries(node, start, stop)
        node.start = start                      if node.start.nil? or node.start < start
        node.stop  = node.start + node.min_size if node.stop.nil?  or node.stop  < node.start
        node.stop  = stop                       if node.stop > stop
      end
    end
  end
end
