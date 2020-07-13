module Elasticsearch
  module API
    module Snapshot
      module Actions

        # Return information about specific (or all) snapshots
        #
        # @example Return information about the `snapshot-2` snapshot
        #
        #     client.snapshot.get repository: 'my-backup', snapshot: 'snapshot-2'
        #
        # @example Return information about multiple snapshots
        #
        #     client.snapshot.get repository: 'my-backup', snapshot: ['snapshot-2', 'snapshot-3']
        #
        # @example Return information about all snapshots in the repository
        #
        #     client.snapshot.get repository: 'my-backup', snapshot: '_all'
        #
        # @option arguments [String] :repository A repository name (*Required*)
        # @option arguments [List] :snapshot A comma-separated list of snapshot names (*Required*)
        # @option arguments [Boolean] :ignore_unavailable Whether to ignore unavailable snapshots, defaults to #                                                 false which means a SnapshotMissingException is thrown
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        # @option arguments [Number,List] :ignore The list of HTTP errors to ignore
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/modules-snapshots.html
        #
        def get(arguments={})
          raise ArgumentError, "Required argument 'repository' missing" unless arguments[:repository]
          raise ArgumentError, "Required argument 'snapshot' missing"   unless arguments[:snapshot]

          valid_params = [
            :ignore_unavailable,
            :master_timeout ]

          repository = arguments.delete(:repository)
          snapshot   = arguments.delete(:snapshot)

          method = HTTP_GET
          path   = Utils.__pathify( '_snapshot', Utils.__escape(repository), Utils.__listify(snapshot) )

          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          if Array(arguments[:ignore]).include?(404)
            Utils.__rescue_from_not_found { perform_request(method, path, params, body).body }
          else
            perform_request(method, path, params, body).body
          end
        end
      end
    end
  end
end
