require 'spec_helper'

describe Clamp::Parameter::Definition do

  context "normal" do

    let(:parameter) do
      described_class.new("COLOR", "hue of choice")
    end

    it "has a name" do
      expect(parameter.name).to eql "COLOR"
    end

    it "has a description" do
      expect(parameter.description).to eql "hue of choice"
    end

    it "is single-valued" do
      expect(parameter).to_not be_multivalued
    end

    describe "#attribute_name" do

      it "is derived from the name" do
        expect(parameter.attribute_name).to eql "color"
      end

      it "can be overridden" do
        parameter = described_class.new("COLOR", "hue of choice", :attribute_name => "hue")
        expect(parameter.attribute_name).to eql "hue"
      end

    end

    describe "#consume" do

      it "consumes one argument" do
        arguments = %w(a b c)
        expect(parameter.consume(arguments)).to eql ["a"]
        expect(arguments).to eql %w(b c)
      end

      describe "with no arguments" do

        it "raises an Argument error" do
          arguments = []
          expect do
            parameter.consume(arguments)
          end.to raise_error(ArgumentError)
        end

      end

    end

  end

  context "optional (name in square brackets)" do

    let(:parameter) do
      described_class.new("[COLOR]", "hue of choice")
    end

    it "is single-valued" do
      expect(parameter).to_not be_multivalued
    end

    describe "#attribute_name" do

      it "omits the brackets" do
        expect(parameter.attribute_name).to eql "color"
      end

    end

    describe "#consume" do

      it "consumes one argument" do
        arguments = %w(a b c)
        expect(parameter.consume(arguments)).to eql ["a"]
        expect(arguments).to eql %w(b c)
      end

      describe "with no arguments" do

        it "consumes nothing" do
          arguments = []
          expect(parameter.consume(arguments)).to eql []
        end

      end

    end

  end

  context "list (name followed by ellipsis)" do

    let(:parameter) do
      described_class.new("FILE ...", "files to process")
    end

    it "is multi-valued" do
      expect(parameter).to be_multivalued
    end

    describe "#attribute_name" do

      it "gets a _list suffix" do
        expect(parameter.attribute_name).to eql "file_list"
      end

    end

    describe "#append_method" do

      it "is derived from the attribute_name" do
        expect(parameter.append_method).to eql "append_to_file_list"
      end

    end

    describe "#consume" do

      it "consumes all the remaining arguments" do
        arguments = %w(a b c)
        expect(parameter.consume(arguments)).to eql %w(a b c)
        expect(arguments).to eql []
      end

      describe "with no arguments" do

        it "raises an Argument error" do
          arguments = []
          expect do
            parameter.consume(arguments)
          end.to raise_error(ArgumentError)
        end

      end

    end

    context "with a weird parameter name, and an explicit attribute_name" do

      let(:parameter) do
        described_class.new("KEY=VALUE ...", "config-settings", :attribute_name => :config_settings)
      end

      describe "#attribute_name" do

        it "is the specified one" do
          expect(parameter.attribute_name).to eql "config_settings"
        end

      end

    end

  end

  context "optional list" do

    let(:parameter) do
      described_class.new("[FILES] ...", "files to process")
    end

    it "is multi-valued" do
      expect(parameter).to be_multivalued
    end

    describe "#attribute_name" do

      it "gets a _list suffix" do
        expect(parameter.attribute_name).to eql "files_list"
      end

    end

    describe "#default_value" do

      it "is an empty list" do
        expect(parameter.default_value).to eql []
      end

    end

    describe "#help" do

      it "does not include default" do
        expect(parameter.help_rhs).to_not include("default:")
      end

    end

    context "with specified default value" do

      let(:parameter) do
        described_class.new("[FILES] ...", "files to process", :default => %w(a b c))
      end

      describe "#default_value" do

        it "is that specified" do
          expect(parameter.default_value).to eql %w(a b c)
        end

      end

      describe "#help" do

        it "includes the default value" do
          expect(parameter.help_rhs).to include("default:")
        end

      end

      describe "#consume" do

        it "consumes all the remaining arguments" do
          arguments = %w(a b c)
          expect(parameter.consume(arguments)).to eql %w(a b c)
          expect(arguments).to eql []
        end

        context "with no arguments" do

          it "don't override defaults" do
            arguments = []
            expect(parameter.consume(arguments)).to eql []
          end

        end

      end

    end

  end

end
