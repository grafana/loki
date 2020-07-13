module Elasticsearch
  module API
    module Snapshot
      module Actions

        # Create a repository for storing snapshots
        #
        # @example Create a repository at `/tmp/backup`
        #
        #     client.snapshot.create_repository repository: 'my-backups',
        #                                       body: {
        #                                         type: 'fs',
        #                                         settings: { location: '/tmp/backup', compress: true  }
        #                                       }
        #
        # @option arguments [String] :repository A repository name (*Required*)
        # @option arguments [Hash] :body The repository definition (*Required*)
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        # @option arguments [Time] :timeout Explicit operation timeout
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/modules-snapshots.html#_repositories
        #
        def create_repository(arguments={})
          raise ArgumentError, "Required argument 'repository' missing" unless arguments[:repository]
          raise ArgumentError, "Required argument 'body' missing"       unless arguments[:body]
          valid_params = [
            :repository,
            :master_timeout,
            :timeout ]

          repository = arguments.delete(:repository)

          method = HTTP_PUT
          path   = Utils.__pathify( '_snapshot', Utils.__escape(repository) )

          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = arguments[:body]

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
