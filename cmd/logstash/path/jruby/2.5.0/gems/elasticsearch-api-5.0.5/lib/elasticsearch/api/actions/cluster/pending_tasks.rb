module Elasticsearch
  module API
    module Cluster
      module Actions

        # Returns a list of any cluster-level changes (e.g. create index, update mapping, allocate or fail shard)
        # which have not yet been executed and are queued up.
        #
        # @example Get a list of currently queued up tasks in the cluster
        #
        #     client.cluster.pending_tasks
        #
        # @option arguments [Boolean] :local Return local information, do not retrieve the state from master node
        #                                    (default: false)
        # @option arguments [Time] :master_timeout Specify timeout for connection to master
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/cluster-pending.html
        #
        def pending_tasks(arguments={})
          valid_params = [
            :local,
            :master_timeout ]
          method = HTTP_GET
          path   = "_cluster/pending_tasks"
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
