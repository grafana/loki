module Elasticsearch
  module API
    module Cat
      module Actions

        # Shows all snapshots that belong to a specific repository
        #
        # @example Return snapshots for 'my_repository'
        #
        #     client.cat.snapshots repository: 'my_repository'
        #
        # @example Return id, status and start_epoch for 'my_repository'
        #
        #     client.cat.snapshots repository: 'my_repository', h: 'id,status,start_epoch'
        #
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        # @option arguments [List] :h Comma-separated list of column names to display
        # @option arguments [Boolean] :help Return help information
        # @option arguments [Boolean] :v Verbose mode. Display column headers
        # @option arguments [List] :s Comma-separated list of column names or column aliases to sort by
        #
        # @see https://www.elastic.co/guide/en/elasticsearch/reference/current/cat-snapshots.html
        #
        def snapshots(arguments={})
          raise ArgumentError, "Required argument 'repository' missing" if !arguments[:repository] && !arguments[:help]

          valid_params = [
            :master_timeout,
            :h,
            :help,
            :v,
            :s ]

          repository = arguments.delete(:repository)

          method = HTTP_GET
          path   = Utils.__pathify "_cat/snapshots", Utils.__escape(repository)
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end

