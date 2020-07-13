module Concurrent
  module Actor
    module Behaviour
      # TODO track what is linked, clean when :terminated
      #   send :linked/:unlinked messages back to build the array of linked actors

      # Links the actor to other actors and sends actor's events to them,
      # like: `:terminated`, `:paused`, `:resumed`, errors, etc.
      # Linked actor needs to handle those messages.
      #
      #     listener = AdHoc.spawn name: :listener do
      #       lambda do |message|
      #         case message
      #         when Reference
      #           if message.ask!(:linked?)
      #             message << :unlink
      #           else
      #             message << :link
      #           end
      #         else
      #           puts "got event #{message.inspect} from #{envelope.sender}"
      #         end
      #       end
      #     end
      #
      #     an_actor = AdHoc.spawn name: :an_actor, supervise: true, behaviour_definition: Behaviour.restarting_behaviour_definition do
      #       lambda { |message| raise 'failed'}
      #     end
      #
      #     # link the actor
      #     listener.ask(an_actor).wait
      #     an_actor.ask(:fail).wait
      #     # unlink the actor
      #     listener.ask(an_actor).wait
      #     an_actor.ask(:fail).wait
      #     an_actor << :terminate!
      #
      # produces only two events, other events happened after unlinking
      #
      #     got event #<RuntimeError: failed> from #<Concurrent::Actor::Reference /an_actor (Concurrent::Actor::Utils::AdHoc)>
      #     got event :reset from #<Concurrent::Actor::Reference /an_actor (Concurrent::Actor::Utils::AdHoc)>
      class Linking < Abstract
        def initialize(core, subsequent, core_options)
          super core, subsequent, core_options
          @linked = Set.new
          @linked.add Actor.current if core_options[:link] != false
        end

        def on_envelope(envelope)
          case envelope.message
          when :link
            link envelope.sender
          when :unlink
            unlink envelope.sender
          when :linked?
            @linked.include? envelope.sender
          when :linked
            @linked.to_a
          else
            pass envelope
          end
        end

        def link(ref)
          @linked.add(ref)
          true
        end

        def unlink(ref)
          @linked.delete(ref)
          true
        end

        def on_event(public, event)
          event_name, _ = event
          @linked.each { |a| a << event } if public
          @linked.clear if event_name == :terminated
          super public, event
        end
      end
    end
  end
end
