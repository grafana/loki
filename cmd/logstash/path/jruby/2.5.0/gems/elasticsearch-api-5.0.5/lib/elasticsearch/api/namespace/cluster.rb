module Elasticsearch
  module API
    module Cluster
      module Actions; end

      # Client for the "cluster" namespace (includes the {Cluster::Actions} methods)
      #
      class ClusterClient
        include Common::Client, Common::Client::Base, Cluster::Actions
      end

      # Proxy method for {ClusterClient}, available in the receiving object
      #
      def cluster
        @cluster ||= ClusterClient.new(self)
      end

    end
  end
end
