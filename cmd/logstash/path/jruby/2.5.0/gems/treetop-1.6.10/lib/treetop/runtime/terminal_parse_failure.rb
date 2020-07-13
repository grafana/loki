module Treetop
  module Runtime
    class TerminalParseFailure
      attr_reader :index, :expected_string, :unexpected

      def initialize(index, expected_string, unexpected = false)
        @index = index
        @expected_string = expected_string
        @unexpected = unexpected
      end

      def to_s
        "String matching #{expected_string} #{@unexpected ? 'not ' : ''}expected."
      end
    end
  end
end
