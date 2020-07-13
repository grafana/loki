module Elasticsearch
  module API
    module Cluster
      module Actions

        # Get the cluster settings (previously set with {Cluster::Actions#put_settings})
        #
        # @example Get cluster settings
        #
        #     client.cluster.get_settings
        #
        # @option arguments [Boolean] :flat_settings Return settings in flat format (default: false)
        # @option arguments [Boolean] :include_defaults Whether to return all default clusters setting
        #                                               (default: false)
        #
        # @see http://elasticsearch.org/guide/reference/api/admin-cluster-update-settings/
        #
        def get_settings(arguments={})
          valid_params = [
            :flat_settings,
            :include_defaults
          ]

          method = HTTP_GET
          path   = "_cluster/settings"
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
