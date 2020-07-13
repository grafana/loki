module RSpec
  module Wait
    class Target < RSpec::Expectations::ExpectationTarget
      # From: https://github.com/rspec/rspec-expectations/blob/v3.0.0/lib/rspec/expectations/expectation_target.rb#L22
      UndefinedValue = Module.new

      # From: https://github.com/rspec/rspec-expectations/blob/v3.0.0/lib/rspec/expectations/expectation_target.rb#L30-L41
      def self.for(value, block, options = {})
        if UndefinedValue.equal?(value)
          unless block
            raise ArgumentError, "You must pass either an argument or a block to `wait_for`."
          end
          new(block, options)
        elsif block
          raise ArgumentError, "You cannot pass both an argument and a block to `wait_for`."
        else
          new(value, options)
        end
      end

      # From: https://github.com/rspec/rspec-expectations/blob/v3.0.0/lib/rspec/expectations/expectation_target.rb#L25-L27
      def initialize(target, options)
        @wait_options = options
        super(target)
      end

      # From: https://github.com/rspec/rspec-expectations/blob/v3.0.0/lib/rspec/expectations/expectation_target.rb#L53-L54
      def to(matcher = nil, message = nil, &block)
        prevent_operator_matchers(:to, matcher) unless matcher
        with_wait { PositiveHandler.handle_matcher(@target, matcher, message, &block) }
      end

      # From: https://github.com/rspec/rspec-expectations/blob/v3.0.0/lib/rspec/expectations/expectation_target.rb#L66-L67
      def not_to(matcher = nil, message = nil, &block)
        prevent_operator_matchers(:not_to, matcher) unless matcher
        with_wait { NegativeHandler.handle_matcher(@target, matcher, message, &block) }
      end

      alias_method :to_not, :not_to

      private

      def with_wait
        Wait.with_wait(@wait_options) { yield }
      end
    end
  end
end
