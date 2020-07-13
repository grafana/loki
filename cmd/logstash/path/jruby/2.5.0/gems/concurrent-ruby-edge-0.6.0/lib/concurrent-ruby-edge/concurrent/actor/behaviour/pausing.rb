module Concurrent
  module Actor
    module Behaviour

      # Allows to pause actors on errors.
      # When paused all arriving messages are collected and processed after the actor
      # is resumed or reset. Resume will simply continue with next message.
      # Reset also reinitialized context.
      # @note TODO missing example
      class Pausing < Abstract
        def initialize(core, subsequent, core_options)
          super core, subsequent, core_options
          @paused   = false
          @deferred = []
        end

        def paused?
          @paused
        end

        def on_envelope(envelope)
          case envelope.message
          when :pause!
            pause!
          when :paused?
            paused?
          when :resume!
            resume!
          when :reset!
            reset!
          when :restart!
            restart!
          else
            if paused?
              @deferred << envelope
              MESSAGE_PROCESSED
            else
              pass envelope
            end
          end
        end

        def pause!(error = nil)
          do_pause
          broadcast true, error || :paused
          true
        end

        def resume!
          return false unless paused?
          do_resume
          broadcast(true, :resumed)
          true
        end

        def reset!
          return false unless paused?
          broadcast(false, :resetting)
          do_reset
          broadcast(true, :reset)
          true
        end

        def restart!
          return false unless paused?
          broadcast(false, :restarting)
          do_restart
          broadcast(true, :restarted)
          true
        end

        def on_event(public, event)
          event_name, _ = event
          reject_deferred if event_name == :terminated
          super public, event
        end

        private

        def do_pause
          @paused = true
          nil
        end

        def do_resume
          @paused = false
          reschedule_deferred
          nil
        end

        def do_reset
          rebuild_context
          do_resume
          reschedule_deferred
          nil
        end

        def do_restart
          rebuild_context
          reject_deferred
          do_resume
          nil
        end

        def rebuild_context
          core.allocate_context
          core.build_context
          nil
        end

        def reschedule_deferred
          @deferred.each { |envelope| core.schedule_execution { core.process_envelope envelope } }
          @deferred.clear
        end

        def reject_deferred
          @deferred.each { |envelope| reject_envelope envelope }
          @deferred.clear
        end
      end
    end
  end
end
