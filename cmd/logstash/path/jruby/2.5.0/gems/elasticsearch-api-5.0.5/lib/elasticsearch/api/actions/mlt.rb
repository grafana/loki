module Elasticsearch
  module API
    module Actions

      # Return documents similar to the specified one.
      #
      # Performs a `more_like_this` query with the specified document as the input.
      #
      # @example Search for similar documents using the `title` property of document `myindex/mytype/1`
      #
      #     # First, let's setup a synonym-aware analyzer ("quick" <=> "fast")
      #     client.indices.create index: 'myindex', body: {
      #       settings: {
      #         analysis: {
      #           filter: {
      #             synonyms: {
      #               type: 'synonym',
      #               synonyms: [ "quick,fast" ]
      #             }
      #           },
      #           analyzer: {
      #             title_synonym: {
      #               type: 'custom',
      #               tokenizer: 'whitespace',
      #               filter: ['lowercase', 'stop', 'snowball', 'synonyms']
      #             }
      #           }
      #         }
      #       },
      #       mappings: {
      #         mytype: {
      #           properties: {
      #             title: {
      #               type: 'string',
      #               analyzer: 'title_synonym'
      #             }
      #           }
      #         }
      #       }
      #     }
      #
      #     # Index three documents
      #     client.index index: 'myindex', type: 'mytype', id: 1, body: { title: 'Quick Brown Fox'   }
      #     client.index index: 'myindex', type: 'mytype', id: 2, body: { title: 'Slow Black Dog'    }
      #     client.index index: 'myindex', type: 'mytype', id: 3, body: { title: 'Fast White Rabbit' }
      #     client.indices.refresh index: 'myindex'
      #
      #     client.mlt index: 'myindex', type: 'mytype', id: 1, mlt_fields: 'title', min_doc_freq: 1, min_term_freq: 1
      #     # => { ... {"title"=>"Fast White Rabbit"}}]}}
      #
      # @option arguments [String] :id The document ID (*Required*)
      # @option arguments [String] :index The name of the index (*Required*)
      # @option arguments [String] :type The type of the document (use `_all` to fetch
      #                                  the first document matching the ID across all types) (*Required*)
      # @option arguments [Hash] :body A specific search request definition
      # @option arguments [Number] :boost_terms The boost factor
      # @option arguments [Number] :max_doc_freq The word occurrence frequency as count: words with higher occurrence
      #                                          in the corpus will be ignored
      # @option arguments [Number] :max_query_terms The maximum query terms to be included in the generated query
      # @option arguments [Number] :max_word_len The minimum length of the word: longer words will be ignored
      # @option arguments [Number] :min_doc_freq The word occurrence frequency as count: words with lower occurrence
      #                                          in the corpus will be ignored
      # @option arguments [Number] :min_term_freq The term frequency as percent: terms with lower occurence
      #                                           in the source document will be ignored
      # @option arguments [Number] :min_word_len The minimum length of the word: shorter words will be ignored
      # @option arguments [List] :mlt_fields Specific fields to perform the query against
      # @option arguments [Number] :percent_terms_to_match How many terms have to match in order to consider
      #                                                    the document a match (default: 0.3)
      # @option arguments [String] :routing Specific routing value
      # @option arguments [Number] :search_from The offset from which to return results
      # @option arguments [List] :search_indices A comma-separated list of indices to perform the query against
      #                                          (default: the index containing the document)
      # @option arguments [String] :search_query_hint The search query hint
      # @option arguments [String] :search_scroll A scroll search request definition
      # @option arguments [Number] :search_size The number of documents to return (default: 10)
      # @option arguments [String] :search_source A specific search request definition (instead of using the request body)
      # @option arguments [String] :search_type Specific search type (eg. `dfs_then_fetch`, `count`, etc)
      # @option arguments [List] :search_types A comma-separated list of types to perform the query against
      #                                        (default: the same type as the document)
      # @option arguments [List] :stop_words A list of stop words to be ignored
      #
      # @see http://elasticsearch.org/guide/reference/api/more-like-this/
      #
      def mlt(arguments={})
        raise ArgumentError, "Required argument 'index' missing" unless arguments[:index]
        raise ArgumentError, "Required argument 'type' missing"  unless arguments[:type]
        raise ArgumentError, "Required argument 'id' missing"    unless arguments[:id]

        valid_params = [
          :boost_terms,
          :max_doc_freq,
          :max_query_terms,
          :max_word_len,
          :min_doc_freq,
          :min_term_freq,
          :min_word_len,
          :mlt_fields,
          :percent_terms_to_match,
          :routing,
          :search_from,
          :search_indices,
          :search_query_hint,
          :search_scroll,
          :search_size,
          :search_source,
          :search_type,
          :search_types,
          :stop_words ]

        method = HTTP_GET
        path   = Utils.__pathify Utils.__escape(arguments[:index]),
                                 Utils.__escape(arguments[:type]),
                                 Utils.__escape(arguments[:id]),
                                 '_mlt'

        params = Utils.__validate_and_extract_params arguments, valid_params

        [:mlt_fields, :search_indices, :search_types, :stop_words].each do |name|
          params[name] = Utils.__listify(params[name]) if params[name]
        end

        body   = arguments[:body]

        perform_request(method, path, params, body).body
      end
    end
  end
end
