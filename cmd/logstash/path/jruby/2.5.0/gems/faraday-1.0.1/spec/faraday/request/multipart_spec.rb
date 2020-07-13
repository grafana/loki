# frozen_string_literal: true

RSpec.describe Faraday::Request::Multipart do
  let(:conn) do
    Faraday.new do |b|
      b.request :multipart
      b.request :url_encoded
      b.adapter :test do |stub|
        stub.post('/echo') do |env|
          posted_as = env[:request_headers]['Content-Type']
          expect(env[:body]).to be_a_kind_of(Faraday::CompositeReadIO)
          [200, { 'Content-Type' => posted_as }, env[:body].read]
        end
      end
    end
  end

  shared_examples 'a multipart request' do
    it 'generates a unique boundary for each request' do
      response1 = conn.post('/echo', payload)
      response2 = conn.post('/echo', payload)

      b1 = parse_multipart_boundary(response1.headers['Content-Type'])
      b2 = parse_multipart_boundary(response2.headers['Content-Type'])
      expect(b1).to_not eq(b2)
    end
  end

  context 'FilePart: when multipart objects in param' do
    let(:payload) do
      {
        a: 1,
        b: {
          c: Faraday::FilePart.new(__FILE__, 'text/x-ruby', nil,
                                   'Content-Disposition' => 'form-data; foo=1'),
          d: 2
        }
      }
    end
    it_behaves_like 'a multipart request'

    it 'forms a multipart request' do
      response = conn.post('/echo', payload)

      boundary = parse_multipart_boundary(response.headers['Content-Type'])
      result = parse_multipart(boundary, response.body)
      expect(result[:errors]).to be_empty

      part_a, body_a = result.part('a')
      expect(part_a).to_not be_nil
      expect(part_a.filename).to be_nil
      expect(body_a).to eq('1')

      part_bc, body_bc = result.part('b[c]')
      expect(part_bc).to_not be_nil
      expect(part_bc.filename).to eq('multipart_spec.rb')
      expect(part_bc.headers['content-disposition']).to eq(
        'form-data; foo=1; name="b[c]"; filename="multipart_spec.rb"'
      )
      expect(part_bc.headers['content-type']).to eq('text/x-ruby')
      expect(part_bc.headers['content-transfer-encoding']).to eq('binary')
      expect(body_bc).to eq(File.read(__FILE__))

      part_bd, body_bd = result.part('b[d]')
      expect(part_bd).to_not be_nil
      expect(part_bd.filename).to be_nil
      expect(body_bd).to eq('2')
    end
  end

  context 'FilePart: when providing json and IO content in the same payload' do
    let(:io) { StringIO.new('io-content') }
    let(:json) do
      {
        b: 1,
        c: 2
      }.to_json
    end

    let(:payload) do
      {
        json: Faraday::ParamPart.new(json, 'application/json'),
        io: Faraday::FilePart.new(io, 'application/pdf')
      }
    end

    it_behaves_like 'a multipart request'

    it 'forms a multipart request' do
      response = conn.post('/echo', payload)

      boundary = parse_multipart_boundary(response.headers['Content-Type'])
      result = parse_multipart(boundary, response.body)
      expect(result[:errors]).to be_empty

      part_json, body_json = result.part('json')
      expect(part_json).to_not be_nil
      expect(part_json.mime).to eq('application/json')
      expect(part_json.filename).to be_nil
      expect(body_json).to eq(json)

      part_io, body_io = result.part('io')
      expect(part_io).to_not be_nil
      expect(part_io.mime).to eq('application/pdf')
      expect(part_io.filename).to eq('local.path')
      expect(body_io).to eq(io.string)
    end
  end

  context 'FilePart: when multipart objects in array param' do
    let(:payload) do
      {
        a: 1,
        b: [{
          c: Faraday::FilePart.new(__FILE__, 'text/x-ruby'),
          d: 2
        }]
      }
    end

    it_behaves_like 'a multipart request'

    it 'forms a multipart request' do
      response = conn.post('/echo', payload)

      boundary = parse_multipart_boundary(response.headers['Content-Type'])
      result = parse_multipart(boundary, response.body)
      expect(result[:errors]).to be_empty

      part_a, body_a = result.part('a')
      expect(part_a).to_not be_nil
      expect(part_a.filename).to be_nil
      expect(body_a).to eq('1')

      part_bc, body_bc = result.part('b[][c]')
      expect(part_bc).to_not be_nil
      expect(part_bc.filename).to eq('multipart_spec.rb')
      expect(part_bc.headers['content-disposition']).to eq(
        'form-data; name="b[][c]"; filename="multipart_spec.rb"'
      )
      expect(part_bc.headers['content-type']).to eq('text/x-ruby')
      expect(part_bc.headers['content-transfer-encoding']).to eq('binary')
      expect(body_bc).to eq(File.read(__FILE__))

      part_bd, body_bd = result.part('b[][d]')
      expect(part_bd).to_not be_nil
      expect(part_bd.filename).to be_nil
      expect(body_bd).to eq('2')
    end
  end

  context 'UploadIO: when multipart objects in param' do
    let(:payload) do
      {
        a: 1,
        b: {
          c: Faraday::UploadIO.new(__FILE__, 'text/x-ruby', nil,
                                   'Content-Disposition' => 'form-data; foo=1'),
          d: 2
        }
      }
    end
    it_behaves_like 'a multipart request'

    it 'forms a multipart request' do
      response = conn.post('/echo', payload)

      boundary = parse_multipart_boundary(response.headers['Content-Type'])
      result = parse_multipart(boundary, response.body)
      expect(result[:errors]).to be_empty

      part_a, body_a = result.part('a')
      expect(part_a).to_not be_nil
      expect(part_a.filename).to be_nil
      expect(body_a).to eq('1')

      part_bc, body_bc = result.part('b[c]')
      expect(part_bc).to_not be_nil
      expect(part_bc.filename).to eq('multipart_spec.rb')
      expect(part_bc.headers['content-disposition']).to eq(
        'form-data; foo=1; name="b[c]"; filename="multipart_spec.rb"'
      )
      expect(part_bc.headers['content-type']).to eq('text/x-ruby')
      expect(part_bc.headers['content-transfer-encoding']).to eq('binary')
      expect(body_bc).to eq(File.read(__FILE__))

      part_bd, body_bd = result.part('b[d]')
      expect(part_bd).to_not be_nil
      expect(part_bd.filename).to be_nil
      expect(body_bd).to eq('2')
    end
  end

  context 'UploadIO: when providing json and IO content in the same payload' do
    let(:io) { StringIO.new('io-content') }
    let(:json) do
      {
        b: 1,
        c: 2
      }.to_json
    end

    let(:payload) do
      {
        json: Faraday::ParamPart.new(json, 'application/json'),
        io: Faraday::UploadIO.new(io, 'application/pdf')
      }
    end

    it_behaves_like 'a multipart request'

    it 'forms a multipart request' do
      response = conn.post('/echo', payload)

      boundary = parse_multipart_boundary(response.headers['Content-Type'])
      result = parse_multipart(boundary, response.body)
      expect(result[:errors]).to be_empty

      part_json, body_json = result.part('json')
      expect(part_json).to_not be_nil
      expect(part_json.mime).to eq('application/json')
      expect(part_json.filename).to be_nil
      expect(body_json).to eq(json)

      part_io, body_io = result.part('io')
      expect(part_io).to_not be_nil
      expect(part_io.mime).to eq('application/pdf')
      expect(part_io.filename).to eq('local.path')
      expect(body_io).to eq(io.string)
    end
  end

  context 'UploadIO: when multipart objects in array param' do
    let(:payload) do
      {
        a: 1,
        b: [{
          c: Faraday::UploadIO.new(__FILE__, 'text/x-ruby'),
          d: 2
        }]
      }
    end

    it_behaves_like 'a multipart request'

    it 'forms a multipart request' do
      response = conn.post('/echo', payload)

      boundary = parse_multipart_boundary(response.headers['Content-Type'])
      result = parse_multipart(boundary, response.body)
      expect(result[:errors]).to be_empty

      part_a, body_a = result.part('a')
      expect(part_a).to_not be_nil
      expect(part_a.filename).to be_nil
      expect(body_a).to eq('1')

      part_bc, body_bc = result.part('b[][c]')
      expect(part_bc).to_not be_nil
      expect(part_bc.filename).to eq('multipart_spec.rb')
      expect(part_bc.headers['content-disposition']).to eq(
        'form-data; name="b[][c]"; filename="multipart_spec.rb"'
      )
      expect(part_bc.headers['content-type']).to eq('text/x-ruby')
      expect(part_bc.headers['content-transfer-encoding']).to eq('binary')
      expect(body_bc).to eq(File.read(__FILE__))

      part_bd, body_bd = result.part('b[][d]')
      expect(part_bd).to_not be_nil
      expect(part_bd.filename).to be_nil
      expect(body_bd).to eq('2')
    end
  end
end
