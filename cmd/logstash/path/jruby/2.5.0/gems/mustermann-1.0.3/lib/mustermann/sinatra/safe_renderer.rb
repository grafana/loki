# frozen_string_literal: true
module Mustermann
  class Sinatra < AST::Pattern
    # Generates a string that can safely be concatenated with other strings
    # without chaning its semantics
    # @see #safe_string
    # @!visibility private
    SafeRenderer = AST::Translator.create do
      translate(:splat, :named_splat) { "{+#{name}}"                           }
      translate(:char, :separator)    { Sinatra.escape(payload)                }
      translate(:root)                { t(payload)                             }
      translate(:group)               { "(#{t(payload)})"                      }
      translate(:union)               { "(#{t(payload, join: ?|)})"            }
      translate(:optional)            { "#{t(payload)}?"                       }
      translate(:with_look_ahead)     { t([head, payload])                     }
      translate(Array)                { |join: ""| map { |e| t(e) }.join(join) }

      translate(:capture) do
        raise Mustermann::Error, 'cannot render variables'      if node.is_a? :variable
        raise Mustermann::Error, 'cannot translate constraints' if constraint or qualifier or convert
        prefix = node.is_a?(:splat) ? "+" : ""
        "{#{prefix}#{name}}"
      end
    end

    private_constant :SafeRenderer
  end
end
