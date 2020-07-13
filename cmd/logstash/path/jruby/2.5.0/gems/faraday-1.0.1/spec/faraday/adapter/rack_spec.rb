# frozen_string_literal: true

RSpec.describe Faraday::Adapter::Rack do
  features :request_body_on_query_methods, :trace_method,
           :skip_response_body_on_head

  it_behaves_like 'an adapter', adapter_options: WebmockRackApp.new
end
