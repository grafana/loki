module Treetop
  module Compiler    
    class CharacterClass < AtomicExpression
      def compile(address, builder, parent_expression = nil)
        super

        builder.if__ "has_terminal?(@regexps[gr = #{grounded_regexp(text_value)}] ||= Regexp.new(gr), :regexp, index)" do
          if address == 0 || decorated?
            assign_result "instantiate_node(#{node_class_name},input, index...(index + 1))"
            extend_result_with_inline_module parent_expression
          else
            assign_lazily_instantiated_node
          end
          builder << "@index += 1"  # Always one character
        end
        builder.else_ do
          builder << "terminal_parse_failure(#{expected})"
          assign_result 'nil'
        end
      end

      def expected
        single_quote('['+characters+']')
      end

      def inline_module
        nil
      end

      def grounded_regexp(string)
        # Double any backslashes, then backslash any single-quotes:
        "'\\A#{string.gsub(/\\/) { '\\\\' }.gsub(/'/) { "\\'"}}'"
      end
    end
  end
end
