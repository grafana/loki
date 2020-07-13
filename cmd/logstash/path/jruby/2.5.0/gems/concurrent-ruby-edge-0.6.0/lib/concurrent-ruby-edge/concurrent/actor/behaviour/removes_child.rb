module Concurrent
  module Actor
    module Behaviour
      # Removes terminated children.
      class RemovesChild < Abstract
        def on_envelope(envelope)
          if envelope.message == :remove_child
            core.remove_child envelope.sender
          else
            pass envelope
          end
        end
      end
    end
  end
end
