require 'concurrent/concern/logging'

module Concurrent
  module Actor
    module Behaviour
      class Abstract
        include TypeCheck
        include InternalDelegations

        attr_reader :core, :subsequent

        def initialize(core, subsequent, core_options)
          @core       = Type! core, Core
          @subsequent = Type! subsequent, Abstract, NilClass
        end

        # override to add extra behaviour
        # @note super needs to be called not to break the chain
        def on_envelope(envelope)
          pass envelope
        end

        # @param [Envelope] envelope to pass to {#subsequent} behaviour
        def pass(envelope)
          subsequent.on_envelope envelope
        end

        # override to add extra behaviour
        # @note super needs to be called not to break the chain
        def on_event(public, event)
          subsequent.on_event public, event if subsequent
        end

        # broadcasts event to all behaviours and context
        # @see #on_event
        # @see AbstractContext#on_event
        def broadcast(public, event)
          core.broadcast(public, event)
        end

        def reject_envelope(envelope)
          envelope.reject! ActorTerminated.new(reference)
          dead_letter_routing << envelope unless envelope.future
          log(DEBUG) { "rejected #{envelope.message} from #{envelope.sender_path}"}
        end
      end
    end
  end
end

