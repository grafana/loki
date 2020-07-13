module Elasticsearch
  module API
    module Indices
      module Actions

        # Get one or more warmers for an index.
        #
        # @example Get all warmers
        #
        #     client.indices.get_warmer index: '_all'
        #
        # @example Get all warmers matching a wildcard expression
        #
        #     client.indices.get_warmer index: '_all', name: 'ba*'
        #
        # @example Get all warmers for a single index
        #
        #     client.indices.get_warmer index: 'foo'
        #
        # @example Get a specific warmer
        #
        #     client.indices.get_warmer index: 'foo', name: 'bar'
        #
        # @option arguments [List] :index A comma-separated list of index names to restrict the operation;
        #                                 use `_all` to perform the operation on all indices (*Required*)
        # @option arguments [String] :name The name of the warmer (supports wildcards); leave empty to get all warmers
        # @option arguments [List] :type A comma-separated list of document types to restrict the operation;
        #                                leave empty to perform the operation on all types
        # @option arguments [Boolean] :allow_no_indices Whether to ignore if a wildcard indices expression resolves into
        #                                               no concrete indices. (This includes `_all` string or when no
        #                                               indices have been specified)
        # @option arguments [String] :expand_wildcards Whether to expand wildcard expression to concrete indices that
        #                                              are open, closed or both. (options: open, closed)
        # @option arguments [String] :ignore_indices When performed on multiple indices, allows to ignore
        #                                            `missing` ones (options: none, missing) @until 1.0
        # @option arguments [Boolean] :ignore_unavailable Whether specified concrete indices should be ignored when
        #                                                 unavailable (missing, closed, etc)
        #
        # @see http://www.elasticsearch.org/guide/reference/api/admin-indices-warmers/
        #
        def get_warmer(arguments={})
          valid_params = [
            :ignore_indices,
            :ignore_unavailable,
            :allow_no_indices,
            :expand_wildcards,
            :local
          ]

          method = HTTP_GET
          path   = Utils.__pathify( Utils.__listify(arguments[:index]), '_warmer', Utils.__escape(arguments[:name]) )
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
