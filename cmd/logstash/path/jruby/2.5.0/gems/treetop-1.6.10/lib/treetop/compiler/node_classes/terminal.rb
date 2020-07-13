module Treetop
  module Compiler    
    class Terminal < AtomicExpression
      def compile(address, builder, parent_expression = nil)
        super
        # Handle modifiers:
        insensitive = modifiers.text_value.include? 'i'
        re = modifiers.text_value.include? 'r'
        if re
          grounded_regexp = "#{('\A'+eval(string)).inspect}"
          cache_key = "'__#{modifiers.text_value}__'+(gr = #{grounded_regexp})"
          re_modifiers = "#{insensitive ? 'Regexp::IGNORECASE' : 0}"
          str = "@regexps[#{cache_key}] ||= Regexp.new(gr, #{re_modifiers})"
          mode = ':regexp'
        elsif insensitive
          str = string.downcase
          string_length = eval(str).length
          mode = ':insens'
        else
          str = string
          string_length = eval(str).length
          mode = 'false'
        end

        builder.if__ "(match_len = has_terminal?(#{str}, #{mode}, index))" do
          if address == 0 || decorated? || mode != 'false' || string_length > 1
            assign_result "instantiate_node(#{node_class_name},input, index...(index + match_len))"
            # debugger if parent_expression and parent_expression.inline_modules.size > 0
            # extend_result_with_inline_module parent_expression
            if parent_expression
              parent_expression.inline_modules.each do |inline|
                extend_result inline.module_name
              end
            end
          else
            assign_lazily_instantiated_node
          end
          builder << "@index += match_len"
        end
        builder.else_ do
          builder << "terminal_parse_failure(#{expected})"
          assign_result 'nil'
        end
      end

      def expected
        single_quote(string)
      end

      def inline_module
        nil
      end
    end
  end
end
