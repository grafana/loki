module Elasticsearch
  module API
    module Indices
      module Actions

        # The rollover index API rolls an alias over to a new index when the existing index
        # is considered to be too large or too old
        #
        # @option arguments [String] :alias The name of the alias to rollover (*Required*)
        # @option arguments [String] :new_index The name of the rollover index
        # @option arguments [Hash] :body The conditions that needs to be met for executing rollover
        # @option arguments [Number] :wait_for_active_shards Wait until the specified number of shards is active
        # @option arguments [Time] :timeout Explicit operation timeout
        # @option arguments [Time] :master_timeout Specify timeout for connection to master
        # @option arguments [Boolean] :dry_run If set to true the rollover action will only be validated but not actually performed even if a condition matches. The default is false
        #
        # @see http://www.elastic.co/guide/en/elasticsearch/reference/master/indices-rollover-index.html
        #
        def rollover(arguments={})
          raise ArgumentError, "Required argument 'alias' missing" unless arguments[:alias]

          valid_params = [
            :wait_for_active_shards,
            :timeout,
            :master_timeout,
            :dry_run ]

          arguments = arguments.clone

          source = arguments.delete(:alias)
          target = arguments.delete(:new_index)

          method = HTTP_POST
          path   = Utils.__pathify Utils.__escape(source), '_rollover', Utils.__escape(target)
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = arguments[:body]

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
