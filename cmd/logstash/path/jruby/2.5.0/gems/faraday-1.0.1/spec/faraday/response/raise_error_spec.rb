# frozen_string_literal: true

RSpec.describe Faraday::Response::RaiseError do
  let(:conn) do
    Faraday.new do |b|
      b.response :raise_error
      b.adapter :test do |stub|
        stub.get('ok') { [200, { 'Content-Type' => 'text/html' }, '<body></body>'] }
        stub.get('bad-request') { [400, { 'X-Reason' => 'because' }, 'keep looking'] }
        stub.get('unauthorized') { [401, { 'X-Reason' => 'because' }, 'keep looking'] }
        stub.get('forbidden') { [403, { 'X-Reason' => 'because' }, 'keep looking'] }
        stub.get('not-found') { [404, { 'X-Reason' => 'because' }, 'keep looking'] }
        stub.get('proxy-error') { [407, { 'X-Reason' => 'because' }, 'keep looking'] }
        stub.get('conflict') { [409, { 'X-Reason' => 'because' }, 'keep looking'] }
        stub.get('unprocessable-entity') { [422, { 'X-Reason' => 'because' }, 'keep looking'] }
        stub.get('4xx') { [499, { 'X-Reason' => 'because' }, 'keep looking'] }
        stub.get('nil-status') { [nil, { 'X-Reason' => 'nil' }, 'fail'] }
        stub.get('server-error') { [500, { 'X-Error' => 'bailout' }, 'fail'] }
      end
    end
  end

  it 'raises no exception for 200 responses' do
    expect { conn.get('ok') }.not_to raise_error
  end

  it 'raises Faraday::BadRequestError for 400 responses' do
    expect { conn.get('bad-request') }.to raise_error(Faraday::BadRequestError) do |ex|
      expect(ex.message).to eq('the server responded with status 400')
      expect(ex.response[:headers]['X-Reason']).to eq('because')
      expect(ex.response[:status]).to eq(400)
    end
  end

  it 'raises Faraday::UnauthorizedError for 401 responses' do
    expect { conn.get('unauthorized') }.to raise_error(Faraday::UnauthorizedError) do |ex|
      expect(ex.message).to eq('the server responded with status 401')
      expect(ex.response[:headers]['X-Reason']).to eq('because')
      expect(ex.response[:status]).to eq(401)
    end
  end

  it 'raises Faraday::ForbiddenError for 403 responses' do
    expect { conn.get('forbidden') }.to raise_error(Faraday::ForbiddenError) do |ex|
      expect(ex.message).to eq('the server responded with status 403')
      expect(ex.response[:headers]['X-Reason']).to eq('because')
      expect(ex.response[:status]).to eq(403)
    end
  end

  it 'raises Faraday::ResourceNotFound for 404 responses' do
    expect { conn.get('not-found') }.to raise_error(Faraday::ResourceNotFound) do |ex|
      expect(ex.message).to eq('the server responded with status 404')
      expect(ex.response[:headers]['X-Reason']).to eq('because')
      expect(ex.response[:status]).to eq(404)
    end
  end

  it 'raises Faraday::ProxyAuthError for 407 responses' do
    expect { conn.get('proxy-error') }.to raise_error(Faraday::ProxyAuthError) do |ex|
      expect(ex.message).to eq('407 "Proxy Authentication Required"')
      expect(ex.response[:headers]['X-Reason']).to eq('because')
      expect(ex.response[:status]).to eq(407)
    end
  end

  it 'raises Faraday::ConflictError for 409 responses' do
    expect { conn.get('conflict') }.to raise_error(Faraday::ConflictError) do |ex|
      expect(ex.message).to eq('the server responded with status 409')
      expect(ex.response[:headers]['X-Reason']).to eq('because')
      expect(ex.response[:status]).to eq(409)
    end
  end

  it 'raises Faraday::UnprocessableEntityError for 422 responses' do
    expect { conn.get('unprocessable-entity') }.to raise_error(Faraday::UnprocessableEntityError) do |ex|
      expect(ex.message).to eq('the server responded with status 422')
      expect(ex.response[:headers]['X-Reason']).to eq('because')
      expect(ex.response[:status]).to eq(422)
    end
  end

  it 'raises Faraday::NilStatusError for nil status in response' do
    expect { conn.get('nil-status') }.to raise_error(Faraday::NilStatusError) do |ex|
      expect(ex.message).to eq('http status could not be derived from the server response')
      expect(ex.response[:headers]['X-Reason']).to eq('nil')
      expect(ex.response[:status]).to be_nil
    end
  end

  it 'raises Faraday::ClientError for other 4xx responses' do
    expect { conn.get('4xx') }.to raise_error(Faraday::ClientError) do |ex|
      expect(ex.message).to eq('the server responded with status 499')
      expect(ex.response[:headers]['X-Reason']).to eq('because')
      expect(ex.response[:status]).to eq(499)
    end
  end

  it 'raises Faraday::ServerError for 500 responses' do
    expect { conn.get('server-error') }.to raise_error(Faraday::ServerError) do |ex|
      expect(ex.message).to eq('the server responded with status 500')
      expect(ex.response[:headers]['X-Error']).to eq('bailout')
      expect(ex.response[:status]).to eq(500)
    end
  end
end
