# frozen_string_literal: true

RSpec.describe Faraday::Middleware do
  subject { described_class.new(app) }

  describe '#close' do
    context "with app that doesn't support \#close" do
      let(:app) { double }

      it 'should issue warning' do
        expect(subject).to receive(:warn)
        subject.close
      end
    end

    context "with app that supports \#close" do
      let(:app) { double }

      it 'should issue warning' do
        expect(app).to receive(:close)
        expect(subject).to_not receive(:warn)
        subject.close
      end
    end
  end
end
