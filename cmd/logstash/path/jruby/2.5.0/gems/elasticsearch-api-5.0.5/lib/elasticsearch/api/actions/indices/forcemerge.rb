module Elasticsearch
  module API
    module Indices
      module Actions

        # Force merge an index, list of indices, or all indices in the cluster.
        #
        # @example Fully force merge an index
        #
        #     client.indices.forcemerge index: 'foo', max_num_segments: 1
        #
        # @example Do not flush index after force-merging
        #
        #     client.indices.forcemerge index: 'foo', flush: false
        #
        # @example Do not expunge deleted documents after force-merging
        #
        #     client.indices.forcemerge index: 'foo', only_expunge_deletes: false
        #
        # @example Force merge a list of indices
        #
        #     client.indices.forcemerge index: ['foo', 'bar']
        #     client.indices.forcemerge index: 'foo,bar'
        #
        # @example forcemerge a list of indices matching wildcard expression
        #
        #     client.indices.forcemerge index: 'foo*'
        #
        # @example forcemerge all indices
        #
        #     client.indices.forcemerge index: '_all'
        #
        # @option arguments [List] :index A comma-separated list of indices to forcemerge;
        #                                 use `_all` to forcemerge all indices
        # @option arguments [Number] :max_num_segments The number of segments the index should be merged into
        #                                              (default: dynamic)
        # @option arguments [Boolean] :only_expunge_deletes Specify whether the operation should only expunge
        #                                                   deleted documents
        # @option arguments [Boolean] :flush Specify whether the index should be flushed after performing the operation
        #                                    (default: true)
        #
        # @see http://www.elastic.co/guide/en/elasticsearch/reference/master/indices-forcemerge.html
        #
        def forcemerge(arguments={})
          valid_params = [
            :max_num_segments,
            :only_expunge_deletes,
            :flush
          ]

          method = HTTP_POST
          path   = Utils.__pathify Utils.__listify(arguments[:index]), '_forcemerge'

          params = Utils.__validate_and_extract_params arguments, valid_params
          body = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
