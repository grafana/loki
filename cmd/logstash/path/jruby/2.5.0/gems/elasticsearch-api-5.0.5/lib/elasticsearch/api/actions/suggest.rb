module Elasticsearch
  module API
    module Actions

      # Return query terms suggestions based on provided text and configuration.
      #
      # Pass the request definition in the `:body` argument.
      #
      # @example Return query terms suggestions ("auto-correction")
      #
      #     client.suggest index: 'myindex',
      #                    body: { my_suggest: { text: 'tset', term: { field: 'title' } } }
      #     # => { ... "my_suggest"=>[ {"text"=>"tset", ... "options"=>[{"text"=>"test", "score"=>0.75, "freq"=>5}] }]}
      #
      # @option arguments [List] :index A comma-separated list of index names to restrict the operation;
      #                                 use `_all` or empty string to perform the operation on all indices
      # @option arguments [Hash] :body The request definition
      # @option arguments [String] :ignore_indices When performed on multiple indices, allows to ignore `missing` ones
      #                                            (options: none, missing)
      # @option arguments [String] :preference Specify the node or shard the operation should be performed on
      #                                        (default: random)
      # @option arguments [String] :routing Specific routing value
      # @option arguments [String] :source The URL-encoded request definition (instead of using request body)
      #
      # @since 0.90
      #
      # @see http://elasticsearch.org/guide/reference/api/search/suggest/
      #
      def suggest(arguments={})
        valid_params = [
          :ignore_indices,
          :preference,
          :routing,
          :source ]

        method = HTTP_POST
        path   = Utils.__pathify( Utils.__listify(arguments[:index]), '_suggest' )

        params = Utils.__validate_and_extract_params arguments, valid_params
        body   = arguments[:body]

        perform_request(method, path, params, body).body
      end
    end
  end
end
