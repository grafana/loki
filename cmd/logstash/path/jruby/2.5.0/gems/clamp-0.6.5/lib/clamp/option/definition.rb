require 'clamp/attribute/definition'
require 'clamp/truthy'

module Clamp
  module Option

    class Definition < Attribute::Definition

      def initialize(switches, type, description, options = {})
        @switches = Array(switches)
        @type = type
        @description = description
        super(options)
        @multivalued = options[:multivalued]
        if options.has_key?(:required)
          @required = options[:required]
          # Do some light validation for conflicting settings.
          if options.has_key?(:default)
            raise ArgumentError, "Specifying a :default value also :required doesn't make sense"
          end
          if type == :flag
            raise ArgumentError, "A required flag (boolean) doesn't make sense."
          end
        end
      end

      attr_reader :switches, :type

      def long_switch
        switches.find { |switch| switch =~ /^--/ }
      end

      def handles?(switch)
        recognised_switches.member?(switch)
      end

      def flag?
        @type == :flag
      end

      def flag_value(switch)
        !(switch =~ /^--no-(.*)/ && switches.member?("--\[no-\]#{$1}"))
      end

      def read_method
        if flag?
          super + "?"
        else
          super
        end
      end

      def extract_value(switch, arguments)
        if flag?
          flag_value(switch)
        else
          arguments.shift
        end
      end

      def default_conversion_block
        if flag?
          Clamp.method(:truthy?)
        end
      end

      def help_lhs
        lhs = switches.join(", ")
        lhs += " " + type unless flag?
        lhs
      end

      private

      def recognised_switches
        switches.map do |switch|
          if switch =~ /^--\[no-\](.*)/
            ["--#{$1}", "--no-#{$1}"]
          else
            switch
          end
        end.flatten
      end

      def infer_attribute_name
        inferred_name = long_switch.sub(/^--(\[no-\])?/, '').tr('-', '_')
        inferred_name += "_list" if multivalued?
        inferred_name
      end

    end

  end
end
