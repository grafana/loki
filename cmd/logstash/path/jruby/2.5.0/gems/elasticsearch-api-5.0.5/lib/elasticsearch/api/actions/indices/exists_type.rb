module Elasticsearch
  module API
    module Indices
      module Actions

        # Return true if the specified type exists, false otherwise.
        #
        #     client.indices.exists_type? index: 'myindex', type: 'mytype'
        #
        # @option arguments [List] :index A comma-separated list of index names; use `_all`
        #                                 to check the types across all indices (*Required*)
        # @option arguments [List] :type A comma-separated list of document types to check (*Required*)
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
        # @see https://www.elastic.co/guide/en/elasticsearch/reference/current/indices-types-exists.html
        #
        def exists_type(arguments={})
          raise ArgumentError, "Required argument 'index' missing" unless arguments[:index]
          raise ArgumentError, "Required argument 'type' missing" unless arguments[:type]
          valid_params = [
            :ignore_indices,
            :ignore_unavailable,
            :allow_no_indices,
            :expand_wildcards,
            :local
          ]

          method = HTTP_HEAD
          path   = Utils.__pathify Utils.__listify(arguments[:index]), '_mapping', Utils.__escape(arguments[:type])

          params = Utils.__validate_and_extract_params arguments, valid_params
          body = nil

          Utils.__rescue_from_not_found do
            perform_request(method, path, params, body).status == 200 ? true : false
          end
        end

        alias_method :exists_type?, :exists_type
      end
    end
  end
end
