module Elasticsearch
  module API
    module Actions

      # Pre-render search requests before they are executed and fill existing templates with template parameters
      #
      # @option arguments [String] :id The id of the stored search template
      # @option arguments [Hash] :body The search definition template and its params
      #
      # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/search-template.html
      #
      def render_search_template(arguments={})
        valid_params = [
          :id
        ]
        method = 'GET'
        path   = "_render/template"
        params = Utils.__validate_and_extract_params arguments, valid_params
        body   = arguments[:body]

        perform_request(method, path, params, body).body
      end
    end
  end
end
