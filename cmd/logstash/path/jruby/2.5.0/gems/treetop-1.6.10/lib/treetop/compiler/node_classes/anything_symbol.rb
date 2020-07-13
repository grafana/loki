module Treetop
  module Compiler    
    class AnythingSymbol < AtomicExpression
      def compile(address, builder, parent_expression = nil)
        super
        builder.if__ "index < input_length" do
          if address == 0 || decorated?
            assign_result "instantiate_node(#{node_class_name},input, index...(index + 1))"
            extend_result_with_inline_module parent_expression
          else
            assign_lazily_instantiated_node
          end
          builder << "@index += 1"
        end
        builder.else_ do
          builder << 'terminal_parse_failure("any character")'
          assign_result 'nil'
        end
      end

      def expected
        '"any character"'
      end

      def inline_module
        nil
      end
    end
  end
end
