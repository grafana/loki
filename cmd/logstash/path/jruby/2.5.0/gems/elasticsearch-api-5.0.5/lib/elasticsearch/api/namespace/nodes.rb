module Elasticsearch
  module API
    module Nodes
      module Actions; end

      # Client for the "nodes" namespace (includes the {Nodes::Actions} methods)
      #
      class NodesClient
        include Common::Client, Common::Client::Base, Nodes::Actions
      end

      # Proxy method for {NodesClient}, available in the receiving object
      #
      def nodes
        @nodes ||= NodesClient.new(self)
      end

    end
  end
end
