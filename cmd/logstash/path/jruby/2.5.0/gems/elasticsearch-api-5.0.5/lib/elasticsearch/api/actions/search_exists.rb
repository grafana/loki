module Elasticsearch
  module API
    module Actions

      # Return whether documents exists for a particular query
      #
      # @option arguments [List] :index A comma-separated list of indices to restrict the results
      # @option arguments [List] :type A comma-separated list of types to restrict the results
      # @option arguments [Hash] :body A query to restrict the results specified with the Query DSL (optional)
      # @option arguments [Boolean] :ignore_unavailable Whether specified concrete indices should be ignored when
      #                                                 unavailable (missing or closed)
      # @option arguments [Boolean] :allow_no_indices Whether to ignore if a wildcard indices expression resolves
      #                                               into no concrete indices.
      # @option arguments [String] :expand_wildcards Whether to expand wildcard expression to concrete indices
      #                                              that are open, closed or both.
      #                                              (options: open, closed, none, all)
      # @option arguments [Number] :min_score Include only documents with a specific `_score` value in the result
      # @option arguments [String] :preference Specify the node or shard the operation should be performed on
      #                                        (default: random)
      # @option arguments [String] :routing Specific routing value
      # @option arguments [String] :q Query in the Lucene query string syntax
      # @option arguments [String] :analyzer The analyzer to use for the query string
      # @option arguments [Boolean] :analyze_wildcard Specify whether wildcard and prefix queries should be
      #                                               analyzed (default: false)
      # @option arguments [String] :default_operator The default operator for query string query (AND or OR)
      #                                              (options: AND, OR)
      # @option arguments [String] :df The field to use as default where no field prefix is given
      #                                in the query string
      # @option arguments [Boolean] :lenient Specify whether format-based query failures
      #                                      (such as providing text to a numeric field) should be ignored
      # @option arguments [Boolean] :lowercase_expanded_terms Specify whether query terms should be lowercased
      #
      # @see http://www.elastic.co/guide/en/elasticsearch/reference/master/search-exists.html
      #
      def search_exists(arguments={})
        valid_params = [
          :ignore_unavailable,
          :allow_no_indices,
          :expand_wildcards,
          :min_score,
          :preference,
          :routing,
          :q,
          :analyzer,
          :analyze_wildcard,
          :default_operator,
          :df,
          :lenient,
          :lowercase_expanded_terms ]
        method = 'POST'
        path   = "_search/exists"
        params = Utils.__validate_and_extract_params arguments, valid_params
        body   = arguments[:body]

        perform_request(method, path, params, body).body
      end
    end
  end
end
