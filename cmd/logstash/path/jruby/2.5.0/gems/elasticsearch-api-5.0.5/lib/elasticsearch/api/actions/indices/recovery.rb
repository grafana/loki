module Elasticsearch
  module API
    module Indices
      module Actions

        # Return information about shard recovery for one or more indices
        #
        # @example Get recovery information for a single index
        #
        #     client.indices.recovery index: 'foo'
        #
        # @example Get detailed recovery information for multiple indices
        #
        #     client.indices.recovery index: ['foo', 'bar'], detailed: true
        #
        # @example Get recovery information for all indices
        #
        #     client.indices.recovery
        #
        # @option arguments [List] :index A comma-separated list of index names; use `_all` or empty string to perform the operation on all indices
        # @option arguments [Boolean] :detailed Whether to display detailed information about shard recovery
        # @option arguments [Boolean] :active_only Display only those recoveries that are currently on-going
        # @option arguments [Boolean] :human Whether to return time and byte values in human readable format
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/indices-recovery.html
        #
        def recovery(arguments={})
          valid_params = [
            :detailed,
            :active_only,
            :human ]
          method = HTTP_GET
          path   = Utils.__pathify Utils.__listify(arguments[:index]), '_recovery'
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
