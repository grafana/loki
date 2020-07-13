module Elasticsearch
  module API
    module Indices
      module Actions

        # Copy an existing index into a new index with a fewer number of primary shards
        #
        # @option arguments [String] :index The name of the source index to shrink (*Required*)
        # @option arguments [String] :target The name of the target index to shrink into (*Required*)
        # @option arguments [Hash] :body The configuration for the target index (`settings` and `aliases`)
        # @option arguments [Number] :wait_for_active_shards Wait until the specified number of shards is active
        # @option arguments [Boolean] :wait_for_no_relocating_shards Whether to wait until there are no relocating
        #                                                            shards in the cluster
        # @option arguments [Time] :timeout Explicit operation timeout
        # @option arguments [Time] :master_timeout Specify timeout for connection to master
        #
        # @see http://www.elastic.co/guide/en/elasticsearch/reference/master/indices-shrink-index.html
        #
        def shrink(arguments={})
          raise ArgumentError, "Required argument 'index' missing" unless arguments[:index]
          raise ArgumentError, "Required argument 'target' missing" unless arguments[:target]

          valid_params = [
            :wait_for_active_shards,
            :wait_for_no_relocating_shards,
            :timeout,
            :master_timeout
          ]

          arguments = arguments.clone

          source = arguments.delete(:index)
          target = arguments.delete(:target)

          method = HTTP_PUT
          path   = Utils.__pathify(source, '_shrink', target)
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = arguments[:body]

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
