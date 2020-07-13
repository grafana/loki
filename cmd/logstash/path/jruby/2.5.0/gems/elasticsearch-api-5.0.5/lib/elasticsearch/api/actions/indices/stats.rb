module Elasticsearch
  module API
    module Indices
      module Actions

        # Return statistical information about one or more indices.
        #
        # The response contains comprehensive statistical information about metrics related to index:
        # how much time did indexing, search and other operations take, how much disk space it takes,
        # how much memory filter caches or field data require, etc.
        #
        # @example Get all available statistics for all indices
        #
        #     client.indices.stats
        #
        # @example Get statistics for a single index
        #
        #     client.indices.stats index: 'foo'
        #
        # @example Get statistics about documents and disk size for multiple indices
        #
        #     client.indices.stats index: ['foo', 'bar'], docs: true, store: true
        #
        # @example Get statistics about filter cache and field data for all fields
        #
        #     client.indices.stats fielddata: true, filter_cache: true
        #
        # @example Get statistics about filter cache and field data for specific fields
        #
        #     client.indices.stats fielddata: true, filter_cache: true, fields: 'created_at,tags'
        #
        # @example Get statistics about filter cache and field data per field for all fields
        #
        #     client.indices.stats fielddata: true, filter_cache: true, fields: '*'
        #
        # @example Get statistics about searches, with segmentation for different search groups
        #
        #     client.indices.stats search: true, groups: ['groupA', 'groupB']
        #
        # @option arguments [Boolean] :docs Return information about indexed and deleted documents
        # @option arguments [Boolean] :fielddata Return information about field data
        # @option arguments [Boolean] :fields A comma-separated list of fields for `fielddata` metric (supports wildcards)
        # @option arguments [Boolean] :filter_cache Return information about filter cache
        # @option arguments [Boolean] :flush Return information about flush operations
        # @option arguments [Boolean] :get Return information about get operations
        # @option arguments [Boolean] :groups A comma-separated list of search groups for `search` statistics
        # @option arguments [Boolean] :id_cache Return information about ID cache
        # @option arguments [List] :index A comma-separated list of index names; use `_all` or empty string
        #                                 to perform the operation on all indices
        # @option arguments [Boolean] :indexing Return information about indexing operations
        # @option arguments [String] :level Return stats aggregated at cluster, index or shard level
        #                                   (Options: cluster, indices, shards)
        # @option arguments [List] :types A comma-separated list of document types to include in the `indexing` info
        # @option arguments [Boolean] :merge Return information about merge operations
        # @option arguments [List] :metric Limit the information returned the specific metrics
        #                                  (_all, completion, docs, fielddata, filter_cache, flush, get,
        #                                  id_cache, indexing, merge, percolate, refresh, search, segments,
        #                                  store, warmer, suggest)
        # @option arguments [Boolean] :refresh Return information about refresh operations
        # @option arguments [Boolean] :search Return information about search operations; use the `groups` parameter to
        #                                     include information for specific search groups
        # @option arguments [List] :groups A comma-separated list of search groups to include in the `search` statistics
        # @option arguments [Boolean] :suggest Return information about suggest statistics
        # @option arguments [Boolean] :store Return information about the size of the index
        # @option arguments [Boolean] :warmer Return information about warmers
        # @option arguments [Boolean] :ignore_unavailable Whether specified concrete indices should be ignored when
        #                                                 unavailable (missing, closed, etc)
        # @option arguments [Boolean] :allow_no_indices Whether to ignore if a wildcard indices expression resolves into
        #                                               no concrete indices. (This includes `_all` string or when no
        #                                               indices have been specified)
        # @option arguments [String] :expand_wildcards Whether to expand wildcard expression to concrete indices that
        #                                              are open, closed or both. (options: open, closed)
        #
        # @option arguments [Boolean] :include_segment_file_sizes Whether to report the aggregated disk usage of each one of the Lucene index files. Only applies if segment stats are requested. (default: false)
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/current/indices-stats.html
        #
        def stats(arguments={})
          valid_parts = [
            :docs,
            :fielddata,
            :filter_cache,
            :flush,
            :get,
            :indexing,
            :merge,
            :metric,
            :refresh,
            :search,
            :suggest,
            :store,
            :warmer ]

          valid_params = [
            :fields,
            :completion_fields,
            :fielddata_fields,
            :groups,
            :level,
            :types,
            :ignore_indices,
            :ignore_unavailable,
            :allow_no_indices,
            :expand_wildcards,
            :include_segment_file_sizes ]

          method = HTTP_GET

          parts  = Utils.__extract_parts arguments, valid_parts
          path   = Utils.__pathify Utils.__listify(arguments[:index]), '_stats', Utils.__listify(parts)

          params = Utils.__validate_and_extract_params arguments, valid_params
          params[:fields] = Utils.__listify(params[:fields], :escape => false) if params[:fields]
          params[:groups] = Utils.__listify(params[:groups], :escape => false) if params[:groups]

          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
