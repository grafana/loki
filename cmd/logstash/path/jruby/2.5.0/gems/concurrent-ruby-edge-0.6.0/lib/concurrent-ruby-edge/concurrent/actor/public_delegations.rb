module Concurrent
  module Actor

    # Provides publicly expose-able methods from {Core}.
    module PublicDelegations
      # @see Core#name
      def name
        core.name
      end

      # @see Core#path
      def path
        core.path
      end

      # @see Core#parent
      def parent
        core.parent
      end

      # @see Core#reference
      def reference
        core.reference
      end

      # @see Core#executor
      def executor
        core.executor
      end

      # @see Core#context_class
      def context_class
        core.context_class
      end

      alias_method :ref, :reference
      alias_method :actor_class, :context_class
    end
  end
end
