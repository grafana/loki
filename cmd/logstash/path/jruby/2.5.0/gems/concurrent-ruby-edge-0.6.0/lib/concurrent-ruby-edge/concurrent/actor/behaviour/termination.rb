module Concurrent
  module Actor
    module Behaviour

      # Handles actor termination. Waits until all its children are terminated,
      # can be configured on behaviour initialization.
      # @note Actor rejects envelopes when terminated.
      # @note TODO missing example
      class Termination < Abstract

        # @!attribute [r] terminated
        #   @return [Edge::Event] event which will become set when actor is terminated.
        attr_reader :terminated

        def initialize(core, subsequent, core_options, trapping = false, terminate_children = true)
          super core, subsequent, core_options
          @terminated         = Concurrent::Promises.resolvable_future
          @public_terminated  = @terminated.with_hidden_resolvable
          @trapping           = trapping
          @terminate_children = terminate_children
        end

        # @note Actor rejects envelopes when terminated.
        # @return [true, false] if actor is terminated
        def terminated?
          @terminated.resolved?
        end

        def trapping?
          @trapping
        end

        def trapping=(val)
          @trapping = !!val
        end

        def on_envelope(envelope)
          command, reason = envelope.message
          case command
          when :terminated?
            terminated?
          when :terminate!
            if trapping? && reason != :kill
              pass envelope
            else
              terminate! reason, envelope
            end
          when :termination_event
            @public_terminated
          else
            if terminated?
              reject_envelope envelope
              MESSAGE_PROCESSED
            else
              pass envelope
            end
          end
        end

        # Terminates the actor. Any Envelope received after termination is rejected.
        # Terminates all its children, does not wait until they are terminated.
        def terminate!(reason = nil, envelope = nil)
          return true if terminated?

          self_termination = Concurrent::Promises.resolved_future(reason.nil?, reason.nil? || nil, reason)
          all_terminations = if @terminate_children
                               Concurrent::Promises.zip(*children.map { |ch| ch.ask(:terminate!) }, self_termination)
                             else
                               self_termination
                             end

          all_terminations.chain_resolvable(@terminated)
          if envelope && envelope.future
            all_terminations.chain { |fulfilled, _, t_reason| envelope.future.resolve fulfilled, true, t_reason }
          end

          broadcast(true, [:terminated, reason]) # TODO do not end up in Dead Letter Router
          parent << :remove_child if parent

          MESSAGE_PROCESSED
        end
      end
    end
  end
end
