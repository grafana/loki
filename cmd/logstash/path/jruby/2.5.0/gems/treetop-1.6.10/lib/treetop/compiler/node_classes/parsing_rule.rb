module Treetop
  module Compiler
    class ParsingRule < Runtime::SyntaxNode

      def compile(builder)
        compile_inline_module_declarations(builder)
        generate_method_definition(builder)
      end
      
      def compile_inline_module_declarations(builder)
        parsing_expression.inline_modules.each_with_index do |inline_module, i|
          inline_module.compile(i, builder, self)
          builder.newline
        end
      end
      
      def generate_method_definition(builder)
        builder.reset_addresses
        expression_address = builder.next_address
        result_var = "r#{expression_address}"
        
        builder.method_declaration(method_name) do
          builder.assign 'start_index', 'index'
          generate_cache_lookup(builder)
          builder.newline
          parsing_expression.compile(expression_address, builder)
          builder.newline
          generate_cache_storage(builder, result_var)
          builder.newline          
          builder << result_var
        end
      end
      
      def generate_cache_lookup(builder)
        builder.if_ "node_cache[:#{name}].has_key?(index)" do
          cache_address = "node_cache[:#{name}][index]"
          builder.assign 'cached', cache_address
          builder.if_ "cached" do
            # Handle lazily instantiated nodes:
            builder << "#{cache_address} = cached = SyntaxNode.new(input, index...(index + 1)) if cached == true"
            builder << '@index = cached.interval.end'
          end
          builder << 'return cached'
        end
      end
      
      def generate_cache_storage(builder, result_var)
        builder.assign "node_cache[:#{name}][start_index]", result_var
      end
      
      def method_name
        "_nt_#{name}"
      end
      
      def name
        nonterminal.text_value
      end
    end
  end
end
