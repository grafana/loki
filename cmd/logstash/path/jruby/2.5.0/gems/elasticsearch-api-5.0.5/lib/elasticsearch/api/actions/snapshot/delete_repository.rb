module Elasticsearch
  module API
    module Snapshot
      module Actions

        # Delete a specific repository or repositories
        #
        # @example Delete the `my-backups` repository
        #
        #     client.snapshot.delete_repository repository: 'my-backups'
        #
        # @option arguments [List] :repository A comma-separated list of repository names (*Required*)
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        # @option arguments [Time] :timeout Explicit operation timeout
        # @option arguments [Number,List] :ignore The list of HTTP errors to ignore
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/modules-snapshots.html
        #
        def delete_repository(arguments={})
          raise ArgumentError, "Required argument 'repository' missing" unless arguments[:repository]

          valid_params = [
            :master_timeout,
            :timeout ]

          repository = arguments.delete(:repository)

          method = HTTP_DELETE
          path   = Utils.__pathify( '_snapshot', Utils.__listify(repository) )

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
