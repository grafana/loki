# frozen_string_literal: true

unless 0.respond_to?(:positive?)
  class Fixnum # rubocop:disable Lint/UnifiedInteger, Style/Documentation
    def positive?
      self > 0 # rubocop:disable Style/NumericPredicate
    end
  end
end
