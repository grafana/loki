module Elasticsearch
  module API
    module Indices
      module Actions

        # Return the mapping definitions for all indices, or specific indices/types.
        #
        # @example Get all mappings in the cluster
        #
        #     client.indices.get_mapping
        #
        # @example Get mapping for a specific index
        #
        #     client.indices.get_mapping index: 'foo'
        #
        # @example Get mapping for a specific type in a specific index
        #
        #     client.indices.get_mapping index: 'foo', type: 'baz'
        #
        # @option arguments [List] :index A comma-separated list of index names; use `_all` or empty string for all indices
        # @option arguments [List] :type A comma-separated list of document types
        # @option arguments [Boolean] :allow_no_indices Whether to ignore if a wildcard indices expression resolves into
        #                                               no concrete indices. (This includes `_all` string or when no
        #                                               indices have been specified)
        # @option arguments [String] :expand_wildcards Whether to expand wildcard expression to concrete indices that
        #                                              are open, closed or both. (options: open, closed)
        # @option arguments [String] :ignore_indices When performed on multiple indices, allows to ignore
        #                                            `missing` ones (options: none, missing) @until 1.0
        # @option arguments [Boolean] :ignore_unavailable Whether specified concrete indices should be ignored when
        #                                                 unavailable (missing, closed, etc)
        # @option arguments [Boolean] :local Return local information, do not retrieve the state from master node
        #                                    (default: false)
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/current/indices-get-mapping.html
        #
        def get_mapping(arguments={})
          valid_params = [
            :ignore_indices,
            :ignore_unavailable,
            :allow_no_indices,
            :expand_wildcards,
            :local
          ]

          method = HTTP_GET
          path   = Utils.__pathify Utils.__listify(arguments[:index]),
                                   '_mapping',
                                   Utils.__listify(arguments[:type])
          params = Utils.__validate_and_extract_params arguments, valid_params
          body = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
