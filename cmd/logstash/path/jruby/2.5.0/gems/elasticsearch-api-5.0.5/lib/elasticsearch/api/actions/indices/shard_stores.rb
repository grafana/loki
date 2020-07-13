module Elasticsearch
  module API
    module Indices
      module Actions

        # Provides low-level information about shards (allocated nodes, exceptions, ...)
        #
        # @option arguments [List] :index A comma-separated list of index names; use `_all` or empty string to perform the operation on all indices
        # @option arguments [List] :status A comma-separated list of statuses used to filter on shards to get store information for (options: green, yellow, red, all)
        # @option arguments [Boolean] :ignore_unavailable Whether specified concrete indices should be ignored when unavailable (missing or closed)
        # @option arguments [Boolean] :allow_no_indices Whether to ignore if a wildcard indices expression resolves into no concrete indices. (This includes `_all` string or when no indices have been specified)
        # @option arguments [String] :expand_wildcards Whether to expand wildcard expression to concrete indices that are open, closed or both. (options: open, closed, none, all)
        # @option arguments [String] :operation_threading
        #
        # @see http://www.elastic.co/guide/en/elasticsearch/reference/master/indices-shards-stores.html
        #
        def shard_stores(arguments={})
          valid_params = [
            :status,
            :ignore_unavailable,
            :allow_no_indices,
            :expand_wildcards,
            :operation_threading ]
          method = 'GET'
          path   = Utils.__pathify Utils.__escape(arguments[:index]), "_shard_stores"
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
