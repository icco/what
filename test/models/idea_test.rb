require File.expand_path(File.dirname(__FILE__) + '/../test_config.rb')

describe "Idea Model" do
  it 'can construct a new instance' do
    @idea = Idea.new
    refute_nil @idea
  end
end
