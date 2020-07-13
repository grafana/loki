module RSpec
  module Wait
    class Proxy
      def initialize(options)
        @options = options
      end

      def for(value = Target::UndefinedValue, &block)
        Target.for(value, block, @options)
      end
    end
  end
end
