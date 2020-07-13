# frozen_string_literal: true

RSpec.describe Faraday::RackBuilder do
  # mock handler classes
  (Handler = Struct.new(:app)).class_eval do
    def call(env)
      env[:request_headers]['X-Middleware'] ||= ''
      env[:request_headers]['X-Middleware'] += ":#{self.class.name.split('::').last}"
      app.call(env)
    end
  end

  class Apple < Handler
  end
  class Orange < Handler
  end
  class Banana < Handler
  end

  class Broken < Faraday::Middleware
    dependency 'zomg/i_dont/exist'
  end

  subject { conn.builder }

  context 'with default stack' do
    let(:conn) { Faraday::Connection.new }

    it { expect(subject[0]).to eq(Faraday::Request.lookup_middleware(:url_encoded)) }
    it { expect(subject.adapter).to eq(Faraday::Adapter.lookup_middleware(Faraday.default_adapter)) }
  end

  context 'with custom empty block' do
    let(:conn) { Faraday::Connection.new {} }

    it { expect(subject[0]).to be_nil }
    it { expect(subject.adapter).to eq(Faraday::Adapter.lookup_middleware(Faraday.default_adapter)) }
  end

  context 'with custom adapter only' do
    let(:conn) do
      Faraday::Connection.new do |builder|
        builder.adapter :test do |stub|
          stub.get('/') { |_| [200, {}, ''] }
        end
      end
    end

    it { expect(subject[0]).to be_nil }
    it { expect(subject.adapter).to eq(Faraday::Adapter.lookup_middleware(:test)) }
  end

  context 'with custom handler and adapter' do
    let(:conn) do
      Faraday::Connection.new do |builder|
        builder.use Apple
        builder.adapter :test do |stub|
          stub.get('/') { |_| [200, {}, ''] }
        end
      end
    end

    it 'locks the stack after making a request' do
      expect(subject.locked?).to be_falsey
      conn.get('/')
      expect(subject.locked?).to be_truthy
      expect { subject.use(Orange) }.to raise_error(Faraday::RackBuilder::StackLocked)
    end

    it 'dup stack is unlocked' do
      expect(subject.locked?).to be_falsey
      subject.lock!
      expect(subject.locked?).to be_truthy
      dup = subject.dup
      expect(dup).to eq(subject)
      expect(dup.locked?).to be_falsey
    end

    it 'allows to compare handlers' do
      expect(subject.handlers.first).to eq(Faraday::RackBuilder::Handler.new(Apple))
    end
  end

  context 'when having a single handler' do
    let(:conn) { Faraday::Connection.new {} }

    before { subject.use(Apple) }

    it { expect(subject.handlers).to eq([Apple]) }

    it 'allows rebuilding' do
      subject.build do |builder|
        builder.use(Orange)
      end
      expect(subject.handlers).to eq([Orange])
    end

    it 'allows use' do
      subject.use(Orange)
      expect(subject.handlers).to eq([Apple, Orange])
    end

    it 'allows insert_before' do
      subject.insert_before(Apple, Orange)
      expect(subject.handlers).to eq([Orange, Apple])
    end

    it 'allows insert_after' do
      subject.insert_after(Apple, Orange)
      expect(subject.handlers).to eq([Apple, Orange])
    end

    it 'raises an error trying to use an unregistered symbol' do
      expect { subject.use(:apple) }.to raise_error(Faraday::Error) do |err|
        expect(err.message).to eq(':apple is not registered on Faraday::Middleware')
      end
    end
  end

  context 'with custom registered middleware' do
    let(:conn) { Faraday::Connection.new {} }

    after { Faraday::Middleware.unregister_middleware(:apple) }

    it 'allows to register with constant' do
      Faraday::Middleware.register_middleware(apple: Apple)
      subject.use(:apple)
      expect(subject.handlers).to eq([Apple])
    end

    it 'allows to register with symbol' do
      Faraday::Middleware.register_middleware(apple: :Apple)
      subject.use(:apple)
      expect(subject.handlers).to eq([Apple])
    end

    it 'allows to register with string' do
      Faraday::Middleware.register_middleware(apple: 'Apple')
      subject.use(:apple)
      expect(subject.handlers).to eq([Apple])
    end

    it 'allows to register with Proc' do
      Faraday::Middleware.register_middleware(apple: -> { Apple })
      subject.use(:apple)
      expect(subject.handlers).to eq([Apple])
    end
  end

  context 'when having two handlers' do
    let(:conn) { Faraday::Connection.new {} }

    before do
      subject.use(Apple)
      subject.use(Orange)
    end

    it 'allows insert_before' do
      subject.insert_before(Orange, Banana)
      expect(subject.handlers).to eq([Apple, Banana, Orange])
    end

    it 'allows insert_after' do
      subject.insert_after(Apple, Banana)
      expect(subject.handlers).to eq([Apple, Banana, Orange])
    end

    it 'allows to swap handlers' do
      subject.swap(Apple, Banana)
      expect(subject.handlers).to eq([Banana, Orange])
    end

    it 'allows to delete a handler' do
      subject.delete(Apple)
      expect(subject.handlers).to eq([Orange])
    end
  end

  context 'when having a handler with broken dependency' do
    let(:conn) do
      Faraday::Connection.new do |builder|
        builder.adapter :test do |stub|
          stub.get('/') { |_| [200, {}, ''] }
        end
      end
    end

    before { subject.use(Broken) }

    it 'raises an error while making a request' do
      expect { conn.get('/') }.to raise_error(RuntimeError) do |err|
        expect(err.message).to match(%r{missing dependency for Broken: .+ -- zomg/i_dont/exist})
      end
    end
  end
end
