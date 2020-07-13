module Elasticsearch
  module API
    module Nodes
      module Actions

        # Returns statistical information about nodes in the cluster.
        #
        # @example Return statistics about JVM
        #
        #     client.nodes.stats metric: 'jvm'
        #
        # @example Return statistics about field data structures for all fields
        #
        #     client.nodes.stats metric: 'indices', index_metric: 'fielddata', fields: '*', human: true
        #
        # @option arguments [List] :metric Limit the information returned to the specified metrics
        #                                  (options: _all, breaker, fs, http, indices, jvm, network,
        #                                  os, process, thread_pool, transport)
        # @option arguments [List] :index_metric Limit the information returned for the `indices` metric
        #                                        to the specified index metrics. Used only when
        #                                        `indices` or `all` metric is specified.
        #                                  (options: _all, completion, docs, fielddata, filter_cache, flush, get,
        #                                  id_cache, indexing, merge, percolate, refresh, search, segments, store,
        #                                  warmer)
        # @option arguments [List] :node_id A comma-separated list of node IDs or names to limit
        #                                   the returned information; use `_local` to return information
        #                                   from the node you're connecting to, leave empty to get information
        #                                   from all nodes
        # @option arguments [List] :completion_fields A comma-separated list of fields for `fielddata` and `suggest`
        #                                             index metrics (supports wildcards)
        # @option arguments [List] :fielddata_fields A comma-separated list of fields for `fielddata` index metric
        #                                            (supports wildcards)
        # @option arguments [List] :fields A comma-separated list of fields for `fielddata` and `completion` index
        #                                  metrics (supports wildcards)
        # @option arguments [Boolean] :include_segment_file_sizes Whether to report the aggregated disk usage of each one of the Lucene index files. Only applies if segment stats are requested. (default: false)
        # @option arguments [Boolean] :groups A comma-separated list of search groups for `search` index metric
        # @option arguments [Boolean] :human Whether to return time and byte values in human-readable format
        # @option arguments [String] :level Specify the level for aggregating indices stats
        #                                   (options: node, indices, shards)
        # @option arguments [List] :types A comma-separated list of document types for the `indexing` index metric
        # @option arguments [Time] :timeout Explicit operation timeout
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/cluster-nodes-stats.html
        #
        def stats(arguments={})
          arguments = arguments.clone

          valid_params = [
            :metric,
            :index_metric,
            :node_id,
            :completion_fields,
            :fielddata_fields,
            :fields,
            :include_segment_file_sizes,
            :groups,
            :human,
            :level,
            :types,
            :timeout ]

          node_id = arguments.delete(:node_id)

          path   = Utils.__pathify '_nodes',
                                   Utils.__listify(node_id),
                                   'stats',
                                   Utils.__listify(arguments.delete(:metric)),
                                   Utils.__listify(arguments.delete(:index_metric))

          params = Utils.__validate_and_extract_params arguments, valid_params

          [:completion_fields, :fielddata_fields, :fields, :groups, :types].each do |key|
            params[key] = Utils.__listify(params[key]) if params[key]
          end

          body   = nil

          perform_request(HTTP_GET, path, params, body).body
        end
      end
    end
  end
end
