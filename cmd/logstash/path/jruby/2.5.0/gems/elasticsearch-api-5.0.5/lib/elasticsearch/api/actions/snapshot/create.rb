module Elasticsearch
  module API
    module Snapshot
      module Actions

        # Create a new snapshot in the repository
        #
        # @example Create a snapshot of the whole cluster in the `my-backups` repository
        #
        #     client.snapshot.create repository: 'my-backups', snapshot: 'snapshot-1'
        #
        # @example Create a snapshot for specific indices in the `my-backups` repository
        #
        #     client.snapshot.create repository: 'my-backups',
        #                            snapshot: 'snapshot-2',
        #                            body: { indices: 'foo,bar', ignore_unavailable: true }
        #
        # @option arguments [String] :repository A repository name (*Required*)
        # @option arguments [String] :snapshot A snapshot name (*Required*)
        # @option arguments [Hash] :body The snapshot definition
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        # @option arguments [Boolean] :wait_for_completion Whether the request should block and wait until
        #                                                  the operation has completed
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/modules-snapshots.html#_snapshot
        #
        def create(arguments={})
          raise ArgumentError, "Required argument 'repository' missing" unless arguments[:repository]
          raise ArgumentError, "Required argument 'snapshot' missing"   unless arguments[:snapshot]
          valid_params = [
            :master_timeout,
            :wait_for_completion ]

          repository = arguments.delete(:repository)
          snapshot   = arguments.delete(:snapshot)

          method = HTTP_PUT
          path   = Utils.__pathify( '_snapshot', Utils.__escape(repository), Utils.__escape(snapshot) )

          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = arguments[:body]

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
