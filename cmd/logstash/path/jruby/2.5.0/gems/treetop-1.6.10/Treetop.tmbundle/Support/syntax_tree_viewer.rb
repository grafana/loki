#!/usr/bin/env ruby
require 'rubygems'
require 'treetop'
require "#{ENV["TM_SUPPORT_PATH"]}/lib/ui"

# Monkey-patch SyntaxNode in a gross violation of its privacy.
module Treetop
  module Runtime
    class SyntaxNode
      def interesting_methods
        # Copied from SyntaxNode#inspect
        methods-[extension_modules.last ? extension_modules.last.methods : nil]-self.class.instance_methods
      end
      
      def to_hash
        {
          "class_and_modules" => self.class.to_s.sub(/.*:/,'') + extension_modules.map{|m| "+"+m.to_s.sub(/.*:/,'')}*"",
          "offset" => interval.first,
          "text_value" => text_value,
          "methods" => interesting_methods * ", ",
          "elements" => elements ? elements.map {|e| e.to_hash} : []
        }
      end
    end
  end
end

class SyntaxTreeViewer
  def self.nib_path
    File.dirname(__FILE__) + "/SyntaxTreeViewer.nib"
  end
  
  def self.new_with_unknown_grammar
    parser = if ENV['TM_SCOPE'] =~ /source\.ruby/ && ENV['TM_SELECTED_TEXT']
      eval(ENV['TM_SELECTED_TEXT'])
    else
      parser_file = if ENV['TM_SCOPE'] =~ /source\.treetop/
        ENV['TM_FILEPATH']
      else
        ask_for_grammar
      end
      return unless parser_file
      Treetop.load(parser_file)
    end
    new(parser.new)
  end
  
  def self.ask_for_grammar
    files = TextMate::UI.request_file("title" => "Select a Grammar File")
    if files.nil? || files.empty?
      nil
    else
      files[0]
    end
  end
  
  def self.nib_path
    File.dirname(__FILE__) + "/nibs/SyntaxTreeViewer.nib"
  end
  
  def initialize(_parser)
    @parser = _parser
    run_parser!
  end
  
  def dialog
    TextMate::UI.dialog(
      :nib => self.class.nib_path, 
      :parameters => parameters
    ) do |d|
      d.wait_for_input do |params|
        if params["returnArgument"]
          self.input = params["returnArgument"]
          run_parser!
          d.parameters = parameters
          true
        else
          false
        end
      end
    end
  end
  
  def input
    ENV['TM_TREETOP_SYNTAX_VIEWER_INPUT'] ||= ""
  end
  
  def input=(new_input)
    ENV['TM_TREETOP_SYNTAX_VIEWER_INPUT'] = new_input
  end
  
  private
  def run_parser!
    if input && !input.empty?
      @syntax_tree = @parser.parse(input)
      TextMate::UI.alert(:warning, "Parse error", @parser.failure_reason) unless @syntax_tree
    end
  end
  
  def parameters
    p = {
      "input" => input, 
      "syntax_tree" => @syntax_tree ? @syntax_tree.to_hash : {},
      "selected_tab" => !input.empty? && @syntax_tree ? "Syntax Tree" : "Input"
    }
    puts p
    p
  end
end

if __FILE__ == $0
  Dir.chdir("/Users/aobrien/Work/canopy/play/sparql/")
  require "/Users/aobrien/Work/canopy/play/sparql/lib/sparql"
  v = SyntaxTreeViewer.new(SparqlParser.new)
  v.input = %{PREFIX foaf: <http://xmlns.com/foaf/0.1/> SELECT ?foo WHERE { ?x foaf:knows ?y . ?z foaf:knows ?x .}}
  v.dialog
end
