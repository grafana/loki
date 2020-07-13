module I18n
  module HashRefinements
    refine Hash do
      using I18n::HashRefinements
      def except(*keys)
        dup.except!(*keys)
      end

      def except!(*keys)
        keys.each { |key| delete(key) }
        self
      end

      def deep_symbolize_keys
        each_with_object({}) do |(key, value), result|
          result[symbolize_key(key)] = deep_symbolize_keys_in_object(value)
          result
        end
      end

      # deep_merge from activesupport 5
      # Copyright (c) 2005-2019 David Heinemeier Hansson
      def deep_merge(other_hash, &block)
        dup.deep_merge!(other_hash, &block)
      end

      # deep_merge! from activesupport 5
      # Copyright (c) 2005-2019 David Heinemeier Hansson
      def deep_merge!(other_hash, &block)
        merge!(other_hash) do |key, this_val, other_val|
          if this_val.is_a?(Hash) && other_val.is_a?(Hash)
            this_val.deep_merge(other_val, &block)
          elsif block_given?
            block.call(key, this_val, other_val)
          else
            other_val
          end
        end
      end

      def symbolize_key(key)
        key.respond_to?(:to_sym) ? key.to_sym : key
      end

      private

      def deep_symbolize_keys_in_object(value)
        case value
        when Hash
          value.deep_symbolize_keys
        when Array
          value.map { |e| deep_symbolize_keys_in_object(e) }
        else
          value
        end
      end
    end
  end
end
