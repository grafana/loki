module Elasticsearch
  module API
    module Ingest
      module Actions

        # Delete a speficied pipeline
        #
        # @option arguments [String] :id Pipeline ID (*Required*)
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        # @option arguments [Time] :timeout Explicit operation timeout
        #
        # @see https://www.elastic.co/guide/en/elasticsearch/plugins/master/ingest.html
        #
        def delete_pipeline(arguments={})
          raise ArgumentError, "Required argument 'id' missing" unless arguments[:id]
          valid_params = [
            :master_timeout,
            :timeout ]
          method = 'DELETE'
          path   = Utils.__pathify "_ingest/pipeline", Utils.__escape(arguments[:id])
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
