module Elasticsearch
  module API
    module Ingest
      module Actions

        # Return a specified pipeline
        #
        # @option arguments [String] :id Comma separated list of pipeline ids (wildcards supported).
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        #
        # @see https://www.elastic.co/guide/en/elasticsearch/plugins/master/ingest.html
        #
        def get_pipeline(arguments={})
          valid_params = [
            :master_timeout ]
          method = 'GET'
          path   = Utils.__pathify "_ingest/pipeline", Utils.__escape(arguments[:id])
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
