module Elasticsearch
  module API
    module Indices
      module Actions

        # @option arguments [List] :index A comma-separated list of index names; use `_all` or empty string for all indices
        # @option arguments [Boolean] :ignore_unavailable Whether specified concrete indices should be ignored when unavailable (missing or closed)
        # @option arguments [Boolean] :allow_no_indices Whether to ignore if a wildcard indices expression resolves into no concrete indices. (This includes `_all` string or when no indices have been specified)
        # @option arguments [String] :expand_wildcards Whether to expand wildcard expression to concrete indices that are open, closed or both. (options: open, closed, none, all)
        #
        # @see http://www.elastic.co/guide/en/elasticsearch/reference/master/indices-flush.html
        #
        def flush_synced(arguments={})
          valid_params = [
            :ignore_unavailable,
            :allow_no_indices,
            :expand_wildcards
          ]

          method = HTTP_POST
          path   = Utils.__pathify Utils.__listify(arguments[:index]), '_flush/synced'

          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          if Array(arguments[:ignore]).include?(404)
            Utils.__rescue_from_not_found { perform_request(method, path, params, body).body }
          else
            perform_request(method, path, params, body).body
          end
        end
      end
    end
  end
end
