module Elasticsearch
  module API
    module Actions

      # Store a template for the search definition in Elasticsearch,
      # to be later used with the `search_template` method
      #
      # @option arguments [String] :id Template ID (*Required*)
      # @option arguments [Hash] :body The document (*Required*)
      #
      # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/search-template.html
      #
      def put_template(arguments={})
        raise ArgumentError, "Required argument 'id' missing"   unless arguments[:id]
        raise ArgumentError, "Required argument 'body' missing" unless arguments[:body]
        method = HTTP_PUT
        path   = "_search/template/#{arguments[:id]}"
        params = {}
        body   = arguments[:body]

        perform_request(method, path, params, body).body
      end
    end
  end
end
