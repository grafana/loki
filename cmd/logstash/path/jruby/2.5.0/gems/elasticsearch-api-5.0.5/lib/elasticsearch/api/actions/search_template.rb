module Elasticsearch
  module API
    module Actions

      # Configure the search definition witha template in Mustache and parameters
      #
      # @example Insert the start and end values for the `range` query
      #
      #     client.search_template index: 'myindex',
      #                            body: {
      #                              template: {
      #                                query: {
      #                                  range: {
      #                                    date: { gte: "{{start}}", lte: "{{end}}" }
      #                                  }
      #                                }
      #                              },
      #                              params: { start: "2014-02-01", end: "2014-03-01" }
      #                            }
      #
      # @option arguments [List] :index A comma-separated list of index names to search; use `_all` or empty
      #                                 string to perform the operation on all indices
      # @option arguments [List] :type A comma-separated list of document types to search;
      #                                leave empty to perform the operation on all types
      # @option arguments [Hash] :body The search definition template and its params
      # @option arguments [Boolean] :ignore_unavailable Whether specified concrete indices should be ignored
      #                                                 when unavailable (missing or closed)
      # @option arguments [Boolean] :allow_no_indices Whether to ignore if a wildcard indices expression resolves into
      #                                               no concrete indices. (This includes `_all` string or when no
      #                                               indices have been specified)
      # @option arguments [String] :expand_wildcards Whether to expand wildcard expression to concrete indices
      #                                              that are open, closed or both. (options: open, closed)
      # @option arguments [String] :preference Specify the node or shard the operation should be performed on
      #                                        (default: random)
      # @option arguments [List] :routing A comma-separated list of specific routing values
      # @option arguments [Duration] :scroll Specify how long a consistent view of the index should be maintained
      #                                      for scrolled search
      # @option arguments [String] :search_type Search operation type (options: query_then_fetch, query_and_fetch,
      #                                         dfs_query_then_fetch, dfs_query_and_fetch, count, scan)
      #
      # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/current/search-template.html
      #
      def search_template(arguments={})
        valid_params = [
          :ignore_unavailable,
          :allow_no_indices,
          :expand_wildcards,
          :preference,
          :routing,
          :scroll,
          :search_type ]
        method = HTTP_GET
        path   = Utils.__pathify( Utils.__listify(arguments[:index]), Utils.__listify(arguments[:type]), '_search/template' )
        params = Utils.__validate_and_extract_params arguments, valid_params
        body   = arguments[:body]

        perform_request(method, path, params, body).body
      end
    end
  end
end
