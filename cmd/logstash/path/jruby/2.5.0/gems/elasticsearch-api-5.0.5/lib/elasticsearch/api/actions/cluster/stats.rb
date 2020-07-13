module Elasticsearch
  module API
    module Cluster
      module Actions

        # Returns statistical information about the cluster
        #
        # @option arguments [List] :node_id A comma-separated list of node IDs or names to limit the returned information; use `_local` to return information from the node you're connecting to, leave empty to get information from all nodes
        # @option arguments [Boolean] :flat_settings Return settings in flat format (default: false)
        # @option arguments [Boolean] :human Whether to return time and byte values in human-readable format.
        # @option arguments [Time] :timeout Explicit operation timeout
        #
        # @see http://www.elastic.co/guide/en/elasticsearch/reference/master/cluster-stats.html
        #
        def stats(arguments={})
          valid_params = [
            :flat_settings,
            :human,
            :timeout ]
          method = 'GET'
          path   = "_cluster/stats"
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
