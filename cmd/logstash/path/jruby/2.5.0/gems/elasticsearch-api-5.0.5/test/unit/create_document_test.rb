require 'test_helper'

module Elasticsearch
  module Test
    class IndexDocumentTest < ::Test::Unit::TestCase

      context "Creating a document with the #create method" do
        subject { FakeClient.new }

        should "require the ID" do
          assert_raise ArgumentError do
            subject.create :index => 'foo', :type => 'bar', :id => nil, :body => {:foo => 'bar'}
          end
        end

        should "perform the create request with a specific ID" do
          subject.expects(:perform_request).with do |method, url, params, body|
            assert_equal 'PUT', method
            assert_equal 'foo/bar/123', url
            assert_equal 'create', params[:op_type]
            assert_nil   params[:id]
            assert_equal({:foo => 'bar'}, body)
            true
          end.returns(FakeResponse.new)

          subject.create :index => 'foo', :type => 'bar', :id => '123', :body => {:foo => 'bar'}
        end

        should "URL-escape the parts" do
          subject.expects(:perform_request).with do |method, url, params, body|
            assert_equal 'foo/bar%2Fbam/123', url
            true
          end.returns(FakeResponse.new)

          subject.create :index => 'foo', :type => 'bar/bam', :id => '123', :body => {}
        end
      end

    end
  end
end
