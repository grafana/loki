module Concurrent
  module Actor
    module InternalDelegations
      include PublicDelegations
      include Logger::Severity

      # @see Core#children
      def children
        core.children
      end

      # @see Termination#terminate!
      def terminate!(reason = nil)
        behaviour!(Behaviour::Termination).terminate!(reason)
      end

      # @see Termination#terminated?
      def terminated?
        behaviour!(Behaviour::Termination).terminated?
      end

      # # @see Termination#reason
      # def reason
      #   behaviour!(Behaviour::Termination).reason
      # end

      # delegates to core.log
      # @see Logging#log
      def log(level, message = nil, &block)
        core.log(level, message, &block)
      end

      # @see AbstractContext#dead_letter_routing
      def dead_letter_routing
        context.dead_letter_routing
      end

      def redirect(reference, envelope = self.envelope)
        reference.message(envelope.message, envelope.future)
        Behaviour::MESSAGE_PROCESSED
      end

      # @return [AbstractContext]
      def context
        core.context
      end

      # see Core#behaviour
      def behaviour(behaviour_class)
        core.behaviour(behaviour_class)
      end

      # see Core#behaviour!
      def behaviour!(behaviour_class)
        core.behaviour!(behaviour_class)
      end

    end
  end
end
