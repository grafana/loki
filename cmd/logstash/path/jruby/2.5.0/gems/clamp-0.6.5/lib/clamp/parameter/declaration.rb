require 'clamp/attribute/declaration'
require 'clamp/parameter/definition'

module Clamp
  module Parameter

    module Declaration

      include Clamp::Attribute::Declaration

      def parameters
        @parameters ||= []
      end

      def has_parameters?
        !parameters.empty?
      end

      def parameter(name, description, options = {}, &block)
        Parameter::Definition.new(name, description, options).tap do |parameter|
          parameters << parameter
          define_accessors_for(parameter, &block)
        end
      end

    end

  end
end
