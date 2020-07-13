# frozen_string_literal: true
module Mustermann
  class Sinatra < AST::Pattern
    # Sinatra syntax definition.
    # @!visibility private
    class Parser < AST::Parser
      on(nil, ??, ?)) { |c| unexpected(c) }

      on(?*)  { |c| scan(/\w+/) ? node(:named_splat, buffer.matched) : node(:splat) }
      on(?:)  { |c| node(:capture) { scan(/\w+/) } }
      on(?\\) { |c| node(:char, expect(/./)) }
      on(?()  { |c| node(:group) { read unless scan(?)) } }
      on(?|)  { |c| node(:or) }

      on ?{ do |char|
        current_pos = buffer.pos
        type = scan(?+) ? :named_splat : :capture
        name = expect(/[\w\.]+/)
        if type == :capture && scan(?|)
          buffer.pos = current_pos
          capture = proc do
            start = pos
            match = expect(/(?<capture>[^\|}]+)/)
            node(:capture, match[:capture], start: start)
          end
          grouped_captures = node(:group, [capture[]]) do
            if scan(?|)
              [min_size(pos - 1, pos, node(:or)), capture[]]
            end
          end
          grouped_captures if expect(?})
        else
          type = :splat if type == :named_splat and name == 'splat'
          expect(?})
          node(type, name)
        end
      end

      suffix ?? do |char, element|
        node(:optional, element)
      end
    end

    private_constant :Parser
  end
end
