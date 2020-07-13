module Elasticsearch
  module API
    module Indices
      module Actions

        # Return true if the index (or all indices in a list) exists, false otherwise.
        #
        # @example Check whether index named _myindex_ exists
        #
        #     client.indices.exists? index: 'myindex'
        #
        # @option arguments [List] :index A comma-separated list of indices to check (*Required*)
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
        # @return [true,false]
        #
        # @see http://www.elasticsearch.org/guide/reference/api/admin-indices-indices-exists/
        #
        def exists(arguments={})
          raise ArgumentError, "Required argument 'index' missing" unless arguments[:index]

          valid_params = [
            :ignore_indices,
            :ignore_unavailable,
            :allow_no_indices,
            :expand_wildcards,
            :local
          ]

          method = HTTP_HEAD
          path   = Utils.__listify(arguments[:index])
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          Utils.__rescue_from_not_found do
            perform_request(method, path, params, body).status == 200 ? true : false
          end
        end

        alias_method :exists?, :exists
      end
    end
  end
end
