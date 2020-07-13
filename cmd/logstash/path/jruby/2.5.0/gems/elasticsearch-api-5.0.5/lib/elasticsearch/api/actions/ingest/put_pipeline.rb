module Elasticsearch
  module API
    module Ingest
      module Actions

        # Add or update a specified pipeline
        #
        # @option arguments [String] :id Pipeline ID (*Required*)
        # @option arguments [Hash] :body The ingest definition (*Required*)
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        # @option arguments [Time] :timeout Explicit operation timeout
        #
        # @see https://www.elastic.co/guide/en/elasticsearch/plugins/master/ingest.html
        #
        def put_pipeline(arguments={})
          raise ArgumentError, "Required argument 'id' missing" unless arguments[:id]
          raise ArgumentError, "Required argument 'body' missing" unless arguments[:body]
          valid_params = [
            :master_timeout,
            :timeout ]
          method = 'PUT'
          path   = Utils.__pathify "_ingest/pipeline", Utils.__escape(arguments[:id])

          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = arguments[:body]

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
