require 'spec_helper'

describe Clamp::Option::Definition do

  context "with String argument" do

    let(:option) do
      described_class.new("--key-file", "FILE", "SSH identity")
    end

    it "has a long_switch" do
      expect(option.long_switch).to eql "--key-file"
    end

    it "has a type" do
      expect(option.type).to eql "FILE"
    end

    it "has a description" do
      expect(option.description).to eql "SSH identity"
    end

    describe "#attribute_name" do

      it "is derived from the (long) switch" do
        expect(option.attribute_name).to eql "key_file"
      end

      it "can be overridden" do
        option = described_class.new("--key-file", "FILE", "SSH identity", :attribute_name => "ssh_identity")
        expect(option.attribute_name).to eql "ssh_identity"
      end

    end

    describe "#write_method" do

      it "is derived from the attribute_name" do
        expect(option.write_method).to eql "key_file="
      end

    end

    describe "#default_value" do

      it "defaults to nil" do
        option = described_class.new("-n", "N", "iterations")
        expect(option.default_value).to eql nil
      end

      it "can be overridden" do
        option = described_class.new("-n", "N", "iterations", :default => 1)
        expect(option.default_value).to eql 1
      end

    end

    describe "#help" do

      it "combines switch, type and description" do
        expect(option.help).to eql ["--key-file FILE", "SSH identity"]
      end

    end

  end

  context "flag" do

    let(:option) do
      described_class.new("--verbose", :flag, "Blah blah blah")
    end

    describe "#default_conversion_block" do

      it "converts truthy values to true" do
        expect(option.default_conversion_block.call("true")).to eql true
        expect(option.default_conversion_block.call("yes")).to eql true
      end

      it "converts falsey values to false" do
        expect(option.default_conversion_block.call("false")).to eql false
        expect(option.default_conversion_block.call("no")).to eql false
      end

    end

    describe "#help" do

      it "excludes option argument" do
        expect(option.help).to eql ["--verbose", "Blah blah blah"]
      end

    end

  end

  context "negatable flag" do

    let(:option) do
      described_class.new("--[no-]force", :flag, "Force installation")
    end

    it "handles both positive and negative forms" do
      expect(option.handles?("--force")).to be true
      expect(option.handles?("--no-force")).to be true
    end

    describe "#flag_value" do

      it "returns true for the positive variant" do
        expect(option.flag_value("--force")).to be true
        expect(option.flag_value("--no-force")).to be false
      end

    end

    describe "#attribute_name" do

      it "is derived from the (long) switch" do
        expect(option.attribute_name).to eql "force"
      end

    end

  end

  context "with both short and long switches" do

    let(:option) do
      described_class.new(["-k", "--key-file"], "FILE", "SSH identity")
    end

    it "handles both switches" do
      expect(option.handles?("--key-file")).to be true
      expect(option.handles?("-k")).to be true
    end

    describe "#help" do

      it "includes both switches" do
        expect(option.help).to eql ["-k, --key-file FILE", "SSH identity"]
      end

    end

  end

  context "with an associated environment variable" do

    let(:option) do
      described_class.new("-x", "X", "mystery option", :environment_variable => "APP_X")
    end

    describe "#help" do

      it "describes environment variable" do
        expect(option.help).to eql ["-x X", "mystery option (default: $APP_X)"]
      end

    end

    context "and a default value" do

      let(:option) do
        described_class.new("-x", "X", "mystery option", :environment_variable => "APP_X", :default => "xyz")
      end

      describe "#help" do

        it "describes both environment variable and default" do
          expect(option.help).to eql ["-x X", %{mystery option (default: $APP_X, or "xyz")}]
        end

      end

    end

  end

  context "multivalued" do

    let(:option) do
      described_class.new(["-H", "--header"], "HEADER", "extra header", :multivalued => true)
    end

    it "is multivalued" do
      expect(option).to be_multivalued
    end

    describe "#default_value" do

      it "defaults to an empty Array" do
        expect(option.default_value).to eql []
      end

      it "can be overridden" do
        option = described_class.new("-H", "HEADER", "extra header", :multivalued => true, :default => [1,2,3])
        expect(option.default_value).to eql [1,2,3]
      end

    end

    describe "#attribute_name" do

      it "gets a _list suffix" do
        expect(option.attribute_name).to eql "header_list"
      end

    end

    describe "#append_method" do

      it "is derived from the attribute_name" do
        expect(option.append_method).to eql "append_to_header_list"
      end

    end

  end

  describe "in subcommand" do

    let(:command_class) do

      Class.new(Clamp::Command) do
        subcommand "foo", "FOO!" do
          option "--bar", "BAR", "Bars foo."
        end
      end

    end

    describe "Command#help" do

      it "includes help for each option exactly once" do
        subcommand = command_class.send(:find_subcommand, 'foo')
        subcommand_help = subcommand.subcommand_class.help("")
        expect(subcommand_help.lines.grep(/--bar BAR/).count).to eql 1
      end

    end

  end

  describe "a required option" do
    it "rejects :default" do
      expect do
        described_class.new("--key-file", "FILE", "SSH identity",
                          :required => true, :default => "hello")
      end.to raise_error(ArgumentError)
    end

    it "rejects :flag options" do
      expect do
        described_class.new("--awesome", :flag, "Be awesome?", :required => true)
      end.to raise_error(ArgumentError)
    end
  end
end
