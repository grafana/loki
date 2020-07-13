module Elasticsearch
  module API
    module Actions

      # Returns the names of indices and shards on which a search request would be executed
      #
      # @option arguments [String] :index The name of the index
      # @option arguments [String] :type The type of the document
      # @option arguments [String] :preference Specify the node or shard the operation should be performed on
      #                                        (default: random)
      # @option arguments [String] :routing Specific routing value
      # @option arguments [Boolean] :local Return local information, do not retrieve the state from master node
      #                                    (default: false)
      # @option arguments [Boolean] :ignore_unavailable Whether specified concrete indices should be ignored when
      #                                                 unavailable (missing or closed)
      # @option arguments [Boolean] :allow_no_indices Whether to ignore if a wildcard indices expression resolves
      #                                               into no concrete indices.
      #                                               (This includes `_all` or when no indices have been specified)
      # @option arguments [String] :expand_wildcards Whether to expand wildcard expression to concrete indices
      #                                              that are open, closed or both. (options: open, closed)
      #
      # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/search-shards.html
      #
      def search_shards(arguments={})
        valid_params = [
          :preference,
          :routing,
          :local,
          :ignore_unavailable,
          :allow_no_indices,
          :expand_wildcards ]
        method = HTTP_GET
        path   = Utils.__pathify( Utils.__listify(arguments[:index]), Utils.__listify(arguments[:type]), '_search_shards' )
        params = Utils.__validate_and_extract_params arguments, valid_params
        body   = nil

        perform_request(method, path, params, body).body
      end
    end
  end
end
