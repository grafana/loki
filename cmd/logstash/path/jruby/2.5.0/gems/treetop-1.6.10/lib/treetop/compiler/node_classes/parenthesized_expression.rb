module Treetop
  module Compiler    
    class ParenthesizedExpression < ParsingExpression
      def compile(address, builder, parent_expression = nil)
        elements[2].compile(address, builder, parent_expression)
      end

      def expected
        elements[2].expected
      end
    end
  end
end
