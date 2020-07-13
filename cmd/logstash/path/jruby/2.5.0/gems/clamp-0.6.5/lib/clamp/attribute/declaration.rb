module Clamp
  module Attribute

    module Declaration

      protected

      def define_accessors_for(attribute, &block)
        define_reader_for(attribute)
        define_default_for(attribute)
        if attribute.multivalued?
          define_appender_for(attribute, &block)
          define_multi_writer_for(attribute)
        else
          define_simple_writer_for(attribute, &block)
        end
      end

      def define_reader_for(attribute)
        define_method(attribute.read_method) do
          attribute.of(self)._read
        end
      end

      def define_default_for(attribute)
        define_method(attribute.default_method) do
          attribute.default_value
        end
      end

      def define_simple_writer_for(attribute, &block)
        define_method(attribute.write_method) do |value|
          value = instance_exec(value, &block) if block
          attribute.of(self).set(value)
        end
      end

      def define_appender_for(attribute, &block)
        define_method(attribute.append_method) do |value|
          value = instance_exec(value, &block) if block
          attribute.of(self)._append(value)
        end
      end

      def define_multi_writer_for(attribute)
        define_method(attribute.write_method) do |values|
          attribute.of(self)._replace(values)
        end
      end

    end

  end
end
