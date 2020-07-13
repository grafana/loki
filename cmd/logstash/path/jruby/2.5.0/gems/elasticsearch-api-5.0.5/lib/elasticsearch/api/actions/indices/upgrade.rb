module Elasticsearch
  module API
    module Indices
      module Actions

        # Upgrade the index or indices to the latest Lucene format.
        #
        # @option arguments [List] :index A comma-separated list of index names;
        #                                 use `_all` or empty string to perform the operation on all indices
        # @option arguments [Boolean] :ignore_unavailable Whether specified concrete indices should be ignored
        #                                                 when unavailable (missing or closed)
        # @option arguments [Boolean] :allow_no_indices Whether to ignore if a wildcard indices expression
        #                                               resolves into no concrete indices.
        # @option arguments [String] :expand_wildcards Whether to expand wildcard expression to concrete indices
        #                                              that are open, closed or both. (options: open, closed)
        # @option arguments [Boolean] :wait_for_completion Specify whether the request should block until the all
        #                                                  segments are upgraded (default: true)
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/indices-upgrade.html
        #
        def upgrade(arguments={})
          valid_params = [
            :ignore_unavailable,
            :allow_no_indices,
            :expand_wildcards,
            :wait_for_completion ]

          method = HTTP_POST
          path   = "_upgrade"
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
