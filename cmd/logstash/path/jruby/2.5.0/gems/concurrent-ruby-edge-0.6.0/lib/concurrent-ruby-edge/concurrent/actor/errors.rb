module Concurrent
  module Actor
    Error = Class.new(StandardError)

    class ActorTerminated < Error
      include TypeCheck

      attr_reader :reference

      def initialize(reference)
        @reference = Type! reference, Reference
        super reference.path
      end
    end

    class UnknownMessage < Error
      include TypeCheck

      attr_reader :envelope

      def initialize(envelope)
        @envelope = Type! envelope, Envelope
        super "#{envelope.message.inspect} from #{envelope.sender_path}"
      end
    end
  end
end
