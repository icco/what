require File.expand_path(File.dirname(__FILE__) + '/../test_config.rb')

describe "Comment Model" do
  it 'can construct a new instance' do
    @comment = Comment.new
    refute_nil @comment
  end
end
