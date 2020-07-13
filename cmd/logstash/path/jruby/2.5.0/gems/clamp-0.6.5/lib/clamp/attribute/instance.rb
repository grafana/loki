module Clamp
  module Attribute

    # Represents an option/parameter of a Clamp::Command instance.
    #
    class Instance

      def initialize(attribute, command)
        @attribute = attribute
        @command = command
      end

      attr_reader :attribute, :command

      def defined?
        command.instance_variable_defined?(attribute.ivar_name)
      end

      # get value directly
      def get
        command.instance_variable_get(attribute.ivar_name)
      end

      # set value directly
      def set(value)
        command.instance_variable_set(attribute.ivar_name, value)
      end

      def default
        command.send(attribute.default_method)
      end

      # default implementation of read_method
      def _read
        set(default) unless self.defined?
        get
      end

      # default implementation of append_method
      def _append(value)
        current_values = get || []
        set(current_values + [value])
      end

      # default implementation of write_method for multi-valued attributes
      def _replace(values)
        set([])
        Array(values).each { |value| take(value) }
      end

      def read
        command.send(attribute.read_method)
      end

      def take(value)
        if attribute.multivalued?
          command.send(attribute.append_method, value)
        else
          command.send(attribute.write_method, value)
        end
      end

      def default_from_environment
        return if self.defined?
        return if attribute.environment_variable.nil?
        return unless ENV.has_key?(attribute.environment_variable)
        # Set the parameter value if it's environment variable is present
        value = ENV[attribute.environment_variable]
        begin
          take(value)
        rescue ArgumentError => e
          command.send(:signal_usage_error, "$#{attribute.environment_variable}: #{e.message}")
        end
      end

    end

  end
end
