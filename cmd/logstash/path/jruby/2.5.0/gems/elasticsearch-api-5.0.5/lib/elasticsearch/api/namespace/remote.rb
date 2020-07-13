module Elasticsearch
  module API
    module Remote
      module Actions; end

      # Client for the "remote" namespace (includes the {Remote::Actions} methods)
      #
      class RemoteClient
        include Common::Client, Common::Client::Base, Remote::Actions
      end

      # Proxy method for {RemoteClient}, available in the receiving object
      #
      def remote
        @remote ||= RemoteClient.new(self)
      end

    end
  end
end
