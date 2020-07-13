# frozen_string_literal: true

require 'stringio'

RSpec.describe Faraday::CompositeReadIO do
  Part = Struct.new(:to_io) do
    def length
      to_io.string.length
    end
  end

  def part(str)
    Part.new StringIO.new(str)
  end

  def composite_io(*parts)
    Faraday::CompositeReadIO.new(*parts)
  end

  context 'with empty composite_io' do
    subject { composite_io }

    it { expect(subject.length).to eq(0) }
    it { expect(subject.read).to eq('') }
    it { expect(subject.read(1)).to be_nil }
  end

  context 'with empty parts' do
    subject { composite_io(part(''), part('')) }

    it { expect(subject.length).to eq(0) }
    it { expect(subject.read).to eq('') }
    it { expect(subject.read(1)).to be_nil }
  end

  context 'with 2 parts' do
    subject { composite_io(part('abcd'), part('1234')) }

    it { expect(subject.length).to eq(8) }
    it { expect(subject.read).to eq('abcd1234') }
    it 'allows to read in chunks' do
      expect(subject.read(3)).to eq('abc')
      expect(subject.read(3)).to eq('d12')
      expect(subject.read(3)).to eq('34')
      expect(subject.read(3)).to be_nil
    end
    it 'allows to rewind while reading in chunks' do
      expect(subject.read(3)).to eq('abc')
      expect(subject.read(3)).to eq('d12')
      subject.rewind
      expect(subject.read(3)).to eq('abc')
      expect(subject.read(5)).to eq('d1234')
      expect(subject.read(3)).to be_nil
      subject.rewind
      expect(subject.read(2)).to eq('ab')
    end
  end

  context 'with mix of empty and non-empty parts' do
    subject { composite_io(part(''), part('abcd'), part(''), part('1234'), part('')) }

    it 'allows to read in chunks' do
      expect(subject.read(6)).to eq('abcd12')
      expect(subject.read(6)).to eq('34')
      expect(subject.read(6)).to be_nil
    end
  end

  context 'with utf8 multibyte part' do
    subject { composite_io(part("\x86"), part('ファイル')) }

    it { expect(subject.read).to eq(String.new("\x86\xE3\x83\x95\xE3\x82\xA1\xE3\x82\xA4\xE3\x83\xAB", encoding: 'BINARY')) }
    it 'allows to read in chunks' do
      expect(subject.read(3)).to eq(String.new("\x86\xE3\x83", encoding: 'BINARY'))
      expect(subject.read(3)).to eq(String.new("\x95\xE3\x82", encoding: 'BINARY'))
      expect(subject.read(8)).to eq(String.new("\xA1\xE3\x82\xA4\xE3\x83\xAB", encoding: 'BINARY'))
      expect(subject.read(3)).to be_nil
    end
  end
end
