# frozen_string_literal: true
module Mustermann
  class Sinatra < AST::Pattern
    # Tries to translate objects to Sinatra patterns.
    # @!visibility private
    class TryConvert < AST::Translator
      # @return [Mustermann::Sinatra, nil]
      # @!visibility private
      def self.convert(input, **options)
        new(options).translate(input)
      end

      # Expected options for the resulting pattern.
      # @!visibility private
      attr_reader :options

      # @!visibility private
      def initialize(options)
        @options = options
      end

      # @return [Mustermann::Sinatra]
      # @!visibility private
      def new(input, escape = false)
        input = Mustermann::Sinatra.escape(input) if escape
        Mustermann::Sinatra.new(input, **options)
      end

      # @return [true, false] whether or not expected pattern should have uri_decode option set
      # @!visibility private
      def uri_decode
        options.fetch(:uri_decode, true)
      end

      translate(Object) { nil               }
      translate(String) { t.new(self, true) }

      translate(Identity) { t.new(self, true) if uri_decode == t.uri_decode }
      translate(Sinatra)  { node              if options    == t.options    }

      translate AST::Pattern do
        next unless options == t.options
        t.new(SafeRenderer.translate(to_ast)) rescue nil
      end
    end

    private_constant :TryConvert
  end
end
