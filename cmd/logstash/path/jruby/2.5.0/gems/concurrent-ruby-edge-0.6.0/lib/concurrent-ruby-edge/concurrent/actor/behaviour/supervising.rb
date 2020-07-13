module Concurrent
  module Actor
    module Behaviour

      # Handles supervised actors. Handle configures what to do with failed child: :terminate!, :resume!, :reset!,
      # or :restart!. Strategy sets :one_for_one (restarts just failed actor) or :one_for_all (restarts all child actors).
      # @note TODO missing example
      # @note this will change in next version to support supervision trees better
      class Supervising < Abstract
        def initialize(core, subsequent, core_options, handle, strategy)
          super core, subsequent, core_options
          @handle   = Match! handle, :terminate!, :resume!, :reset!, :restart!
          @strategy = case @handle
                      when :terminate!
                        Match! strategy, nil
                      when :resume!
                        Match! strategy, :one_for_one
                      when :reset!, :restart!
                        Match! strategy, :one_for_one, :one_for_all
                      end
        end

        def on_envelope(envelope)
          case envelope.message
          when Exception, :paused
            receivers = if @strategy == :one_for_all
                          children
                        else
                          [envelope.sender]
                        end
            receivers.each { |ch| ch << @handle }
          else
            pass envelope
          end
        end
      end
    end
  end
end
