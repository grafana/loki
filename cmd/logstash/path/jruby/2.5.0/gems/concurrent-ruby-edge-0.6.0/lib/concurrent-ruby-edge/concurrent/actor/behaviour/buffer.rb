module Concurrent
  module Actor
    module Behaviour

      # Any message reaching this behaviour is buffered. Only one message is is
      # scheduled at any given time. Others are kept in buffer until another one
      # can be scheduled. This effectively means that messages handled by
      # behaviours before buffer have higher priority and they can be processed
      # before messages arriving into buffer. This allows for the processing of
      # internal actor messages like (`:link`, `:supervise`) first.
      class Buffer < Abstract
        def initialize(core, subsequent, core_options)
          super core, subsequent, core_options
          @buffer                     = []
          @receive_envelope_scheduled = false
        end

        def on_envelope(envelope)
          @buffer.push envelope
          process_envelopes?
          MESSAGE_PROCESSED
        end

        # Ensures that only one envelope processing is scheduled with #schedule_execution,
        # this allows other scheduled blocks to be executed before next envelope processing.
        # Simply put this ensures that Core is still responsive to internal calls (like add_child)
        # even though the Actor is flooded with messages.
        def process_envelopes?
          unless @buffer.empty? || @receive_envelope_scheduled
            @receive_envelope_scheduled = true
            process_envelope
          end
        end

        def process_envelope
          envelope = @buffer.shift
          return nil unless envelope
          pass envelope
        ensure
          @receive_envelope_scheduled = false
          core.schedule_execution { process_envelopes? }
        end

        def on_event(public, event)
          event_name, _ = event
          case event_name
          when :terminated, :restarted
            @buffer.each { |envelope| reject_envelope envelope }
            @buffer.clear
          end
          super public, event_name
        end
      end
    end
  end
end
