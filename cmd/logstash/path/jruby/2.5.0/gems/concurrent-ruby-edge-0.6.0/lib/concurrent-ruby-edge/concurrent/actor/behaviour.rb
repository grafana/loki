module Concurrent
  module Actor

    # Actors have modular architecture, which is achieved by combining a light core with chain of
    # behaviours. Each message or internal event propagates through the chain allowing the
    # behaviours react based on their responsibility.
    #
    # -   {Behaviour::Linking}:
    #
    #     > {include:Actor::Behaviour::Linking}
    #
    # -   {Behaviour::Awaits}:
    #
    #     > {include:Actor::Behaviour::Awaits}
    #
    # -   {Behaviour::Pausing}:
    #
    #     > {include:Actor::Behaviour::Pausing}
    #
    # -   {Behaviour::Supervising}:
    #
    #     > {include:Actor::Behaviour::Supervising}
    #
    # -   {Behaviour::Supervising}:
    #
    #     > {include:Actor::Behaviour::Supervising}
    #
    # -   {Behaviour::ExecutesContext}:
    #
    #     > {include:Actor::Behaviour::ExecutesContext}
    #
    # -   {Behaviour::ErrorsOnUnknownMessage}:
    #
    #     > {include:Actor::Behaviour::ErrorsOnUnknownMessage}
    #
    # -   {Behaviour::Termination}:
    #
    #     > {include:Actor::Behaviour::Termination}
    #
    # -   {Behaviour::RemovesChild}:
    #
    #     > {include:Actor::Behaviour::RemovesChild}
    #
    # If needed new behaviours can be added, or old one removed to get required behaviour.
    #
    # -   {Context} uses
    #     {include:Actor::Behaviour.basic_behaviour_definition}
    #
    # -   {RestartingContext} uses
    #     {include:Actor::Behaviour.restarting_behaviour_definition}
    module Behaviour
      MESSAGE_PROCESSED = ::Object.new

      require 'concurrent/actor/behaviour/abstract'
      require 'concurrent/actor/behaviour/awaits'
      require 'concurrent/actor/behaviour/buffer'
      require 'concurrent/actor/behaviour/errors_on_unknown_message'
      require 'concurrent/actor/behaviour/executes_context'
      require 'concurrent/actor/behaviour/linking'
      require 'concurrent/actor/behaviour/pausing'
      require 'concurrent/actor/behaviour/removes_child'
      require 'concurrent/actor/behaviour/sets_results'
      require 'concurrent/actor/behaviour/supervising'
      require 'concurrent/actor/behaviour/termination'

      # Array of behaviours and their construction parameters.
      #
      #     [[Behaviour::SetResults, :terminate!],
      #      [Behaviour::RemovesChild],
      #      [Behaviour::Termination],
      #      [Behaviour::Linking],
      #      [Behaviour::Awaits],
      #      [Behaviour::ExecutesContext],
      #      [Behaviour::ErrorsOnUnknownMessage]]
      #
      # @see '' its source code
      def self.basic_behaviour_definition
        [*base(:terminate!),
         *linking,
         *user_messages]
      end

      # Array of behaviours and their construction parameters.
      #
      #     [[Behaviour::SetResults, :pause!],
      #      [Behaviour::RemovesChild],
      #      [Behaviour::Termination],
      #      [Behaviour::Linking],
      #      [Behaviour::Pausing],
      #      [Behaviour::Supervising, :reset!, :one_for_one],
      #      [Behaviour::Awaits],
      #      [Behaviour::ExecutesContext],
      #      [Behaviour::ErrorsOnUnknownMessage]]
      #
      # @see '' its source code
      def self.restarting_behaviour_definition(handle = :reset!, strategy = :one_for_one)
        [*base(:pause!),
         *linking,
         *supervised,
         *supervising(handle, strategy),
         *user_messages]
      end

      # @see '' its source code
      def self.base(on_error)
        [[SetResults, on_error],
         # has to be before Termination to be able to remove children from terminated actor
         RemovesChild,
         Termination]
      end

      # @see '' its source code
      def self.linking
        [Linking]
      end

      # @see '' its source code
      def self.supervised
        [Pausing]
      end

      # @see '' its source code
      def self.supervising(handle = :reset!, strategy = :one_for_one)
        [[Behaviour::Supervising, handle, strategy]]
      end

      # @see '' its source code
      def self.user_messages
        [Awaits,
         ExecutesContext,
         ErrorsOnUnknownMessage]
      end
    end
  end
end
