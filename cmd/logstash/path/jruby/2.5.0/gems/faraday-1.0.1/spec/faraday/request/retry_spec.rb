# frozen_string_literal: true

RSpec.describe Faraday::Request::Retry do
  let(:calls) { [] }
  let(:times_called) { calls.size }
  let(:options) { [] }
  let(:conn) do
    Faraday.new do |b|
      b.request :retry, *options

      b.adapter :test do |stub|
        %w[get post].each do |method|
          stub.send(method, '/unstable') do |env|
            calls << env.dup
            env[:body] = nil # simulate blanking out response body
            callback.call
          end
        end
      end
    end
  end

  context 'when an unexpected error happens' do
    let(:callback) { -> { raise 'boom!' } }

    before { expect { conn.get('/unstable') }.to raise_error(RuntimeError) }

    it { expect(times_called).to eq(1) }

    context 'and this is passed as a custom exception' do
      let(:options) { [{ exceptions: StandardError }] }

      it { expect(times_called).to eq(3) }
    end
  end

  context 'when an expected error happens' do
    let(:callback) { -> { raise Errno::ETIMEDOUT } }

    before do
      @started = Time.now
      expect { conn.get('/unstable') }.to raise_error(Errno::ETIMEDOUT)
    end

    it { expect(times_called).to eq(3) }

    context 'and legacy max_retry set to 1' do
      let(:options) { [1] }

      it { expect(times_called).to eq(2) }
    end

    context 'and legacy max_retry set to -9' do
      let(:options) { [-9] }

      it { expect(times_called).to eq(1) }
    end

    context 'and new max_retry set to 3' do
      let(:options) { [{ max: 3 }] }

      it { expect(times_called).to eq(4) }
    end

    context 'and new max_retry set to -9' do
      let(:options) { [{ max: -9 }] }

      it { expect(times_called).to eq(1) }
    end

    context 'and both max_retry and interval are set' do
      let(:options) { [{ max: 2, interval: 0.1 }] }

      it { expect(Time.now - @started).to be_within(0.04).of(0.2) }
    end
  end

  context 'when no exception raised' do
    let(:options) { [{ max: 1, retry_statuses: 429 }] }

    before { conn.get('/unstable') }

    context 'and response code is in retry_statuses' do
      let(:callback) { -> { [429, {}, ''] } }

      it { expect(times_called).to eq(2) }
    end

    context 'and response code is not in retry_statuses' do
      let(:callback) { -> { [503, {}, ''] } }

      it { expect(times_called).to eq(1) }
    end
  end

  describe '#calculate_retry_interval' do
    context 'with exponential backoff' do
      let(:options) { { max: 5, interval: 0.1, backoff_factor: 2 } }
      let(:middleware) { Faraday::Request::Retry.new(nil, options) }

      it { expect(middleware.send(:calculate_retry_interval, 5)).to eq(0.1) }
      it { expect(middleware.send(:calculate_retry_interval, 4)).to eq(0.2) }
      it { expect(middleware.send(:calculate_retry_interval, 3)).to eq(0.4) }
    end

    context 'with exponential backoff and max_interval' do
      let(:options) { { max: 5, interval: 0.1, backoff_factor: 2, max_interval: 0.3 } }
      let(:middleware) { Faraday::Request::Retry.new(nil, options) }

      it { expect(middleware.send(:calculate_retry_interval, 5)).to eq(0.1) }
      it { expect(middleware.send(:calculate_retry_interval, 4)).to eq(0.2) }
      it { expect(middleware.send(:calculate_retry_interval, 3)).to eq(0.3) }
      it { expect(middleware.send(:calculate_retry_interval, 2)).to eq(0.3) }
    end

    context 'with exponential backoff and interval_randomness' do
      let(:options) { { max: 2, interval: 0.1, interval_randomness: 0.05 } }
      let(:middleware) { Faraday::Request::Retry.new(nil, options) }

      it { expect(middleware.send(:calculate_retry_interval, 2)).to be_between(0.1, 0.15) }
    end
  end

  context 'when method is not idempotent' do
    let(:callback) { -> { raise Errno::ETIMEDOUT } }

    before { expect { conn.post('/unstable') }.to raise_error(Errno::ETIMEDOUT) }

    it { expect(times_called).to eq(1) }
  end

  describe 'retry_if option' do
    let(:callback) { -> { raise Errno::ETIMEDOUT } }
    let(:options) { [{ retry_if: @check }] }

    it 'retries if retry_if block always returns true' do
      body = { foo: :bar }
      @check = ->(_, _) { true }
      expect { conn.post('/unstable', body) }.to raise_error(Errno::ETIMEDOUT)
      expect(times_called).to eq(3)
      expect(calls.all? { |env| env[:body] == body }).to be_truthy
    end

    it 'does not retry if retry_if block returns false checking env' do
      @check = ->(env, _) { env[:method] != :post }
      expect { conn.post('/unstable') }.to raise_error(Errno::ETIMEDOUT)
      expect(times_called).to eq(1)
    end

    it 'does not retry if retry_if block returns false checking exception' do
      @check = ->(_, exception) { !exception.is_a?(Errno::ETIMEDOUT) }
      expect { conn.post('/unstable') }.to raise_error(Errno::ETIMEDOUT)
      expect(times_called).to eq(1)
    end

    it 'FilePart: should rewind files on retry' do
      io = StringIO.new('Test data')
      filepart = Faraday::FilePart.new(io, 'application/octet/stream')

      rewound = 0
      rewind = -> { rewound += 1 }

      @check = ->(_, _) { true }
      allow(filepart).to receive(:rewind, &rewind)
      expect { conn.post('/unstable', file: filepart) }.to raise_error(Errno::ETIMEDOUT)
      expect(times_called).to eq(3)
      expect(rewound).to eq(2)
    end

    it 'UploadIO: should rewind files on retry' do
      io = StringIO.new('Test data')
      upload_io = Faraday::UploadIO.new(io, 'application/octet/stream')

      rewound = 0
      rewind = -> { rewound += 1 }

      @check = ->(_, _) { true }
      allow(upload_io).to receive(:rewind, &rewind)
      expect { conn.post('/unstable', file: upload_io) }.to raise_error(Errno::ETIMEDOUT)
      expect(times_called).to eq(3)
      expect(rewound).to eq(2)
    end

    context 'when explicitly specifying methods to retry' do
      let(:options) { [{ retry_if: @check, methods: [:post] }] }

      it 'does not call retry_if for specified methods' do
        @check = ->(_, _) { raise 'this should have never been called' }
        expect { conn.post('/unstable') }.to raise_error(Errno::ETIMEDOUT)
        expect(times_called).to eq(3)
      end
    end

    context 'with empty list of methods to retry' do
      let(:options) { [{ retry_if: @check, methods: [] }] }

      it 'calls retry_if for all methods' do
        @check = ->(_, _) { calls.size < 2 }
        expect { conn.get('/unstable') }.to raise_error(Errno::ETIMEDOUT)
        expect(times_called).to eq(2)
      end
    end
  end

  describe 'retry_after header support' do
    let(:callback) { -> { [504, headers, ''] } }
    let(:elapsed) { Time.now - @started }

    before do
      @started = Time.now
      conn.get('/unstable')
    end

    context 'when retry_after bigger than interval' do
      let(:headers) { { 'Retry-After' => '0.5' } }
      let(:options) { [{ max: 1, interval: 0.1, retry_statuses: 504 }] }

      it { expect(elapsed).to be > 0.5 }
    end

    context 'when retry_after smaller than interval' do
      let(:headers) { { 'Retry-After' => '0.1' } }
      let(:options) { [{ max: 1, interval: 0.2, retry_statuses: 504 }] }

      it { expect(elapsed).to be > 0.2 }
    end

    context 'when retry_after is a timestamp' do
      let(:headers) { { 'Retry-After' => (Time.now.utc + 2).strftime('%a, %d %b %Y %H:%M:%S GMT') } }
      let(:options) { [{ max: 1, interval: 0.1, retry_statuses: 504 }] }

      it { expect(elapsed).to be > 1 }
    end

    context 'when retry_after is bigger than max_interval' do
      let(:headers) { { 'Retry-After' => (Time.now.utc + 20).strftime('%a, %d %b %Y %H:%M:%S GMT') } }
      let(:options) { [{ max: 2, interval: 0.1, max_interval: 5, retry_statuses: 504 }] }

      it { expect(times_called).to eq(1) }
    end
  end
end
