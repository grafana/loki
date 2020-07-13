module Elasticsearch
  module API
    module Cluster
      module Actions

        # Update cluster settings.
        #
        # @example Disable shard allocation in the cluster until restart
        #
        #     client.cluster.put_settings body: { transient: { 'cluster.routing.allocation.disable_allocation' => true } }
        #
        # @option arguments [Hash] :body The settings to be updated. Can be either `transient` or `persistent`
        #                                (survives cluster restart).
        #
        # @see http://elasticsearch.org/guide/reference/api/admin-cluster-update-settings/
        #
        def put_settings(arguments={})
          valid_params = [ :flat_settings ]

          method = HTTP_PUT
          path   = "_cluster/settings"
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = arguments[:body] || {}

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
