module Concurrent
  module Actor
    module Utils

      module AsAdHoc
        def initialize(*args, &initializer)
          @on_message = Type! initializer.call(*args), Proc
        end

        def on_message(message)
          instance_exec message, &@on_message
        end
      end

      # Allows quick creation of actors with behaviour defined by blocks.
      # @example ping
      #   AdHoc.spawn :forward, an_actor do |where|
      #     # this block has to return proc defining #on_message behaviour
      #     -> message { where.tell message  }
      #   end
      class AdHoc < Context
        include AsAdHoc
      end
    end
  end
end
