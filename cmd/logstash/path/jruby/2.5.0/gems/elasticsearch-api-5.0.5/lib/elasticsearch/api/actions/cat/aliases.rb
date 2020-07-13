module Elasticsearch
  module API
    module Cat
      module Actions

        # Returns information about aliases, including associated routing values and filters.
        #
        # @example Display all aliases in the cluster
        #
        #     puts client.cat.aliases
        #
        # @example Display indices for the 'year-2013' alias
        #
        #     puts client.cat.aliases name: 'year-2013'
        #
        # @example Display header names in the output
        #
        #     puts client.cat.aliases v: true
        #
        # @example Return only the 'alias' and 'index' columns
        #
        #     puts client.cat.aliases h: ['alias', 'index']
        #
        # @example Return only the 'alias' and 'index' columns, using short names
        #
        #     puts client.cat.aliases h: 'a,i'
        #
        # @example Return the output sorted by the alias name
        #
        #     puts client.cat.aliases s: 'alias'
        #
        # @example Return the information as Ruby objects
        #
        #     client.cat.aliases format: 'json'
        #
        # @option arguments [List] :name A comma-separated list of alias names to return
        # @option arguments [List] :h Comma-separated list of column names to display -- see the `help` argument
        # @option arguments [Boolean] :v Display column headers as part of the output
        # @option arguments [List] :s Comma-separated list of column names or column aliases to sort by
        # @option arguments [String] :format The output format. Options: 'text', 'json'; default: 'text'
        # @option arguments [Boolean] :help Return information about headers
        # @option arguments [Boolean] :local Return local information, do not retrieve the state from master node
        #                                    (default: false)
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/cat-aliases.html
        #
        def aliases(arguments={})
          valid_params = [
            :local,
            :master_timeout,
            :h,
            :help,
            :v,
            :s ]

          name = arguments.delete(:name)

          method = HTTP_GET

          path   = Utils.__pathify '_cat/aliases', Utils.__listify(name)

          params = Utils.__validate_and_extract_params arguments, valid_params
          params[:h] = Utils.__listify(params[:h]) if params[:h]

          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
