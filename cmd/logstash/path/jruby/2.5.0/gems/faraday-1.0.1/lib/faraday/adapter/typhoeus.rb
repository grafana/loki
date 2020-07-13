# frozen_string_literal: true

module Faraday
  class Adapter
    # Typhoeus adapter. This class is just a stub, the real adapter is in
    # https://github.com/typhoeus/typhoeus/blob/master/lib/typhoeus/adapters/faraday.rb
    class Typhoeus < Faraday::Adapter
      # Needs to define this method in order to support Typhoeus <= 1.3.0
      def call; end

      dependency 'typhoeus'
      dependency 'typhoeus/adapters/faraday'
    end
  end
end
