module Elasticsearch
  module API
    module Indices
      module Actions

        # Create or update mapping.
        #
        # Pass the mapping definition(s) in the `:body` argument.
        #
        # @example Create or update a mapping for a specific document type
        #
        #     client.indices.put_mapping index: 'myindex', type: 'mytype', body: {
        #       mytype: {
        #         properties: {
        #           title: { type: 'string', analyzer: 'snowball' }
        #         }
        #       }
        #     }
        #
        # @example Update the mapping for a specific type in all indices
        #
        #     client.indices.put_mapping type: 'mytype', body: {
        #       mytype: {
        #         dynamic: 'strict'
        #       }
        #     }
        #
        # @option arguments [Hash] :body The mapping definition (*Required*)
        # @option arguments [List] :index A comma-separated list of index names; use `_all` or omit to
        #                                 update the mapping for all indices
        # @option arguments [String] :type The name of the document type (*Required*)
        # @option arguments [Boolean] :ignore_conflicts Specify whether to ignore conflicts while updating the mapping
        #                                               (default: false)
        # @option arguments [Boolean] :allow_no_indices Whether to ignore if a wildcard indices expression resolves into
        #                                               no concrete indices. (This includes `_all` string or when no
        #                                               indices have been specified)
        # @option arguments [String] :expand_wildcards Whether to expand wildcard expression to concrete indices that
        #                                              are open, closed or both. (options: open, closed)
        # @option arguments [String] :ignore_indices When performed on multiple indices, allows to ignore
        #                                            `missing` ones (options: none, missing) @until 1.0
        # @option arguments [Boolean] :ignore_unavailable Whether specified concrete indices should be ignored when
        #                                                 unavailable (missing, closed, etc)
        # @option arguments [Boolean] :update_all_types Whether to update the mapping for all fields
        #                                               with the same name across all types
        # @option arguments [Time] :timeout Explicit operation timeout
        # @option arguments [Boolean] :master_timeout Timeout for connection to master
        #
        # @see http://www.elasticsearch.org/guide/reference/api/admin-indices-put-mapping/
        #
        def put_mapping(arguments={})
          raise ArgumentError, "Required argument 'type' missing"  unless arguments[:type]
          raise ArgumentError, "Required argument 'body' missing"  unless arguments[:body]

          valid_params = [
            :ignore_conflicts,
            :ignore_indices,
            :ignore_unavailable,
            :allow_no_indices,
            :expand_wildcards,
            :update_all_types,
            :master_timeout,
            :timeout
          ]

          method = HTTP_PUT
          path   = Utils.__pathify Utils.__listify(arguments[:index]), '_mapping', Utils.__escape(arguments[:type])

          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = arguments[:body]

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
