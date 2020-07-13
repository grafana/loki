# frozen_string_literal: true

# Regexp has `match?` since Ruby 2.4
# so to support Ruby < 2.4 we need to define this method

module Rack
  module RegexpExtensions
    refine Regexp do
      def match?(string, pos = 0)
        !!match(string, pos)
      end
    end unless //.respond_to?(:match?)
  end
end
