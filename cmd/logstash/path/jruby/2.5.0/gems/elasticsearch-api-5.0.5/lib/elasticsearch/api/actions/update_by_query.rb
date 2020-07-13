module Elasticsearch
  module API
    module Actions

      # Process every document matching a query, potentially updating it
      #
      # @example Update all documents in the index, eg. to pick up new mappings
      #
      #     client.update_by_query index: 'articles'
      #
      # @example Update a property of documents matching a query in the index
      #
      #     client.update_by_query index: 'article',
      #                            body: {
      #                              script: { inline: 'ctx._source.views += 1' },
      #                              query: { match: { title: 'foo' } }
      #                            }
      #
      # @option arguments [List] :index A comma-separated list of index names to search; use `_all` or empty string to perform the operation on all indices (*Required*)
      # @option arguments [List] :type A comma-separated list of document types to search; leave empty to perform the operation on all types
      # @option arguments [Hash] :body The search definition using the Query DSL
      # @option arguments [String] :analyzer The analyzer to use for the query string
      # @option arguments [Boolean] :analyze_wildcard Specify whether wildcard and prefix queries should be analyzed (default: false)
      # @option arguments [String] :default_operator The default operator for query string query (AND or OR) (options: AND, OR)
      # @option arguments [String] :df The field to use as default where no field prefix is given in the query string
      # @option arguments [Boolean] :explain Specify whether to return detailed information about score computation as part of a hit
      # @option arguments [List] :fields A comma-separated list of fields to return as part of a hit
      # @option arguments [List] :fielddata_fields A comma-separated list of fields to return as the field data representation of a field for each hit
      # @option arguments [Number] :from Starting offset (default: 0)
      # @option arguments [Boolean] :ignore_unavailable Whether specified concrete indices should be ignored when unavailable (missing or closed)
      # @option arguments [Boolean] :allow_no_indices Whether to ignore if a wildcard indices expression resolves into no concrete indices. (This includes `_all` string or when no indices have been specified)
      # @option arguments [String] :conflicts What to do when the reindex hits version conflicts? (options: abort, proceed)
      # @option arguments [String] :expand_wildcards Whether to expand wildcard expression to concrete indices that are open, closed or both. (options: open, closed, none, all)
      # @option arguments [Boolean] :lenient Specify whether format-based query failures (such as providing text to a numeric field) should be ignored
      # @option arguments [Boolean] :lowercase_expanded_terms Specify whether query terms should be lowercased
      # @option arguments [String] :pipeline Ingest pipeline to set on index requests made by this action. (default: none)
      # @option arguments [String] :preference Specify the node or shard the operation should be performed on (default: random)
      # @option arguments [String] :q Query in the Lucene query string syntax
      # @option arguments [List] :routing A comma-separated list of specific routing values
      # @option arguments [Duration] :scroll Specify how long a consistent view of the index should be maintained for scrolled search
      # @option arguments [String] :search_type Search operation type (options: query_then_fetch, dfs_query_then_fetch)
      # @option arguments [Time] :search_timeout Explicit timeout for each search request. Defaults to no timeout.
      # @option arguments [Number] :size Number of hits to return (default: 10)
      # @option arguments [List] :sort A comma-separated list of <field>:<direction> pairs
      # @option arguments [List] :_source True or false to return the _source field or not, or a list of fields to return
      # @option arguments [List] :_source_exclude A list of fields to exclude from the returned _source field
      # @option arguments [List] :_source_include A list of fields to extract and return from the _source field
      # @option arguments [Number] :terminate_after The maximum number of documents to collect for each shard, upon reaching which the query execution will terminate early.
      # @option arguments [List] :stats Specific 'tag' of the request for logging and statistical purposes
      # @option arguments [String] :suggest_field Specify which field to use for suggestions
      # @option arguments [String] :suggest_mode Specify suggest mode (options: missing, popular, always)
      # @option arguments [Number] :suggest_size How many suggestions to return in response
      # @option arguments [Text] :suggest_text The source text for which the suggestions should be returned
      # @option arguments [Time] :timeout Time each individual bulk request should wait for shards that are unavailable.
      # @option arguments [Boolean] :track_scores Whether to calculate and return scores even if they are not used for sorting
      # @option arguments [Boolean] :version Specify whether to return document version as part of a hit
      # @option arguments [Boolean] :version_type Should the document increment the version number (internal) on hit or not (reindex)
      # @option arguments [Boolean] :request_cache Specify if request cache should be used for this request or not, defaults to index level setting
      # @option arguments [Boolean] :refresh Should the effected indexes be refreshed?
      # @option arguments [String] :consistency Explicit write consistency setting for the operation (options: one, quorum, all)
      # @option arguments [Integer] :scroll_size Size on the scroll request powering the update_by_query
      # @option arguments [Boolean] :wait_for_completion Should the request should block until the reindex is complete.
      # @option arguments [Float] :requests_per_second The throttle for this request in sub-requests per second. 0 means set no throttle.
      #
      # @see https://www.elastic.co/guide/en/elasticsearch/reference/master/docs-update-by-query.html
      #
      def update_by_query(arguments={})
        raise ArgumentError, "Required argument 'index' missing" unless arguments[:index]

        valid_params = [
          :analyzer,
          :analyze_wildcard,
          :default_operator,
          :df,
          :explain,
          :fields,
          :fielddata_fields,
          :from,
          :ignore_unavailable,
          :allow_no_indices,
          :conflicts,
          :expand_wildcards,
          :lenient,
          :lowercase_expanded_terms,
          :pipeline,
          :preference,
          :q,
          :routing,
          :scroll,
          :search_type,
          :search_timeout,
          :size,
          :sort,
          :_source,
          :_source_exclude,
          :_source_include,
          :terminate_after,
          :stats,
          :suggest_field,
          :suggest_mode,
          :suggest_size,
          :suggest_text,
          :timeout,
          :track_scores,
          :version,
          :version_type,
          :request_cache,
          :refresh,
          :consistency,
          :scroll_size,
          :wait_for_completion,
          :requests_per_second ]

        method = HTTP_POST

        path   = Utils.__pathify Utils.__listify(arguments[:index]),
                                 Utils.__listify(arguments[:type]),
                                 '/_update_by_query'

        params = Utils.__validate_and_extract_params arguments, valid_params

        body   = arguments[:body]

        perform_request(method, path, params, body).body
      end
    end
  end
end
