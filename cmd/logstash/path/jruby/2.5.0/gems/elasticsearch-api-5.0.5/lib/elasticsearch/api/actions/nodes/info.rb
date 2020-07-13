module Elasticsearch
  module API
    module Nodes
      module Actions

        # Returns information about nodes in the cluster (cluster settings, JVM version, etc).
        #
        # Use the `all` option to return all available settings, or limit the information returned
        # to a specific type (eg. `http`).
        #
        # Use the `node_id` option to limit information to specific node(s).
        #
        # @example Return information about JVM
        #
        #     client.nodes.info jvm: true
        #
        # @example Return information about HTTP and network
        #
        #     client.nodes.info http: true, network: true
        #
        # @example Pass a list of metrics
        #
        #     client.nodes.info metric: ['http', 'network']
        #
        # @option arguments [List] :node_id A comma-separated list of node IDs or names to limit the returned information;
        #                                   use `_local` to return information from the node you're connecting to, leave
        #                                   empty to get information from all nodes
        # @option arguments [Boolean] :_all Return all available information
        # @option arguments [Boolean] :http Return information about HTTP
        # @option arguments [Boolean] :jvm Return information about the JVM
        # @option arguments [Boolean] :network Return information about network
        # @option arguments [Boolean] :os Return information about the operating system
        # @option arguments [Boolean] :plugins Return information about plugins
        # @option arguments [Boolean] :process Return information about the Elasticsearch process
        # @option arguments [Boolean] :settings Return information about node settings
        # @option arguments [Boolean] :thread_pool Return information about the thread pool
        # @option arguments [Boolean] :transport Return information about transport
        # @option arguments [Boolean] :flat_settings Return settings in flat format (default: false)
        # @option arguments [Time] :timeout Explicit operation timeout
        #
        # @see http://elasticsearch.org/guide/reference/api/admin-cluster-nodes-info/
        #
        def info(arguments={})
          arguments = arguments.clone
          metric    = arguments.delete(:metric)

          valid_parts = [
            :_all,
            :http,
            :jvm,
            :network,
            :os,
            :plugins,
            :process,
            :settings,
            :thread_pool,
            :transport,
            :timeout ]

          valid_params = [ :flat_settings, :timeout ]

          method = HTTP_GET

          if metric
            parts = metric
          else
            parts = Utils.__extract_parts arguments, valid_parts
          end

          path   = Utils.__pathify '_nodes', Utils.__listify(arguments[:node_id]), Utils.__listify(parts)

          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
