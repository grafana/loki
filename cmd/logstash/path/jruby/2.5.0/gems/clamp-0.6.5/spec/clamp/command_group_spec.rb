require 'spec_helper'

describe Clamp::Command do

  extend CommandFactory
  include OutputCapture

  context "with subcommands" do

    given_command "flipflop" do

      def execute
        puts message
      end

      subcommand "flip", "flip it" do
        def message
          "FLIPPED"
        end
      end

      subcommand "flop", "flop it\nfor extra flop" do
        def message
          "FLOPPED"
        end
      end

    end

    it "delegates to sub-commands" do

      command.run(["flip"])
      expect(stdout).to match /FLIPPED/

      command.run(["flop"])
      expect(stdout).to match /FLOPPED/

    end

    context "executed with no subcommand" do

      it "triggers help" do
        expect do
          command.run([])
        end.to raise_error(Clamp::HelpWanted)
      end

    end

    describe "#help" do

      it "shows subcommand parameters in usage" do
        expect(command.help).to include("flipflop [OPTIONS] SUBCOMMAND [ARG] ...")
      end

      it "lists subcommands" do
        help = command.help
        expect(help).to match /Subcommands:/
        expect(help).to match /flip +flip it/
        expect(help).to match /flop +flop it/
      end

      it "handles new lines in subcommand descriptions" do
        expect(command.help).to match /flop +flop it\n +for extra flop/
      end

    end

    describe ".find_subcommand_class" do

      it "finds subcommand classes" do
        flip_class = command_class.find_subcommand_class("flip")
        expect(flip_class.new("xx").message).to eq("FLIPPED")
      end

    end

  end

  context "with an aliased subcommand" do

    given_command "blah" do

      subcommand ["say", "talk"], "Say something" do

        parameter "WORD ...", "stuff to say"

        def execute
          puts word_list
        end

      end

    end

    it "responds to both aliases" do

      command.run(["say", "boo"])
      expect(stdout).to match /boo/

      command.run(["talk", "jive"])
      expect(stdout).to match /jive/

    end

    describe "#help" do

      it "lists all aliases" do
        help = command.help
        expect(help).to match /say, talk .* Say something/
      end

    end

  end

  context "with nested subcommands" do

    given_command "fubar" do

      subcommand "foo", "Foo!" do

        subcommand "bar", "Baaaa!" do

          def self.this_is_bar
          end

          def execute
            puts "FUBAR"
          end

        end

      end

    end

    it "delegates multiple levels" do
      command.run(["foo", "bar"])
      expect(stdout).to match /FUBAR/
    end

    describe ".find_subcommand_class" do

      it "finds nested subcommands" do
        expect(command_class.find_subcommand_class("foo", "bar")).to respond_to(:this_is_bar)
      end

    end

  end

  context "with a default subcommand" do

    given_command "admin" do

      self.default_subcommand = "status"

      subcommand "status", "Show status" do

        def execute
          puts "All good!"
        end

      end

    end

    context "executed with no subcommand" do

      it "invokes the default subcommand" do
        command.run([])
        expect(stdout).to match /All good/
      end

    end

  end

  context "with a default subcommand, declared the old way" do

    given_command "admin" do

      default_subcommand "status", "Show status" do

        def execute
          puts "All good!"
        end

      end

    end

    context "executed with no subcommand" do

      it "invokes the default subcommand" do
        command.run([])
        expect(stdout).to match /All good/
      end

    end

  end

  context "declaring a default subcommand after subcommands" do

    it "is not supported" do

      expect do
        Class.new(Clamp::Command) do

          subcommand "status", "Show status" do

            def execute
              puts "All good!"
            end

          end

          self.default_subcommand = "status"

        end
      end.to raise_error(/default_subcommand must be defined before subcommands/)

    end

  end

  context "with subcommands, declared after a parameter" do

    given_command "with" do

      parameter "THING", "the thing"

      subcommand "spit", "spit it" do
        def execute
          puts "spat the #{thing}"
        end
      end

    end

    it "allows the parameter to be specified first" do

      command.run(["dummy", "spit"])
      expect(stdout.strip).to eql "spat the dummy"

    end

  end

  describe "each subcommand" do

    let(:command_class) do

      speed_options = Module.new do
        extend Clamp::Option::Declaration
        option "--speed", "SPEED", "how fast", :default => "slowly"
      end

      Class.new(Clamp::Command) do

        option "--direction", "DIR", "which way", :default => "home"

        include speed_options

        subcommand "move", "move in the appointed direction" do

          def execute
            motion = context[:motion] || "walking"
            puts "#{motion} #{direction} #{speed}"
          end

        end

      end
    end

    let(:command) do
      command_class.new("go")
    end

    it "accepts options defined in superclass (specified after the subcommand)" do
      command.run(["move", "--direction", "north"])
      expect(stdout).to match /walking north/
    end

    it "accepts options defined in superclass (specified before the subcommand)" do
      command.run(["--direction", "north", "move"])
      expect(stdout).to match /walking north/
    end

    it "accepts options defined in included modules" do
      command.run(["move", "--speed", "very quickly"])
      expect(stdout).to match /walking home very quickly/
    end

    it "has access to command context" do
      command = command_class.new("go", :motion => "wandering")
      command.run(["move"])
      expect(stdout).to match /wandering home/
    end

  end

  context "with a subcommand, with options" do

    given_command 'weeheehee' do
      option '--json', 'JSON', 'a json blob' do |option|
        print "parsing!"
        option
      end

      subcommand 'woohoohoo', 'like weeheehee but with more o' do
        def execute
        end
      end
    end

    it "only parses options once" do
      command.run(['--json', '{"a":"b"}', 'woohoohoo'])
      expect(stdout).to eql 'parsing!'
    end

  end

end
