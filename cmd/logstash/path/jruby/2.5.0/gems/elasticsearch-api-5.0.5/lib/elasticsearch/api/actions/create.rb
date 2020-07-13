module Elasticsearch
  module API
    module Actions

      # Create a document.
      #
      # Enforce the _create_ operation when indexing a document --
      # the operation will return an error when the document already exists.
      #
      # @example Create a document
      #
      #     client.create index: 'myindex',
      #                   type: 'mytype',
      #                   id: '1',
      #                   body: {
      #                    title: 'Test 1',
      #                    tags: ['y', 'z'],
      #                    published: true,
      #                    published_at: Time.now.utc.iso8601,
      #                    counter: 1
      #                   }
      #
      # @option (see Actions#index)
      #
      # (The `:op_type` argument is ignored.)
      #
      # @see http://elasticsearch.org/guide/reference/api/index_/
      #
      def create(arguments={})
        raise ArgumentError, "Required argument 'id' missing"  unless arguments[:id]
        index arguments.update :op_type => 'create'
      end
    end
  end
end
