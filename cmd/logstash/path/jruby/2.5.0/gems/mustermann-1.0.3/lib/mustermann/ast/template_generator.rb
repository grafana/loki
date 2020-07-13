# frozen_string_literal: true
require 'mustermann/ast/translator'

module Mustermann
  module AST
    # Turns an AST into an Array of URI templates representing the AST.
    # @!visibility private
    # @see Mustermann::AST::Pattern#to_templates
    class TemplateGenerator < Translator
      # @!visibility private
      def self.generate_templates(ast)
        new.translate(ast).uniq
      end

      # translate(:expression) is not needed, since template patterns simply call to_s
      translate(:root, :group)        { t(payload) || [""]            }
      translate(:separator, :char)    { t.escape(payload)             }
      translate(:capture)             { "{#{name}}"                   }
      translate(:optional)            { [t(payload), ""]              }
      translate(:named_splat, :splat) { "{+#{name}}"                  }
      translate(:with_look_ahead)     { t([head, payload])            }
      translate(:union)               { payload.flat_map { |e| t(e) } }

      translate(Array) do
        map { |e| Array(t(e)) }.inject { |first, second| first.product(second).map(&:join) }
      end
    end
  end
end
