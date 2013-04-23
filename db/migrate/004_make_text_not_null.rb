class MakeTextNotNull < ActiveRecord::Migration
  def self.up
    Comment.where(:text => nil).delete_all
    Idea.where(:text => nil).delete_all

    change_column_null :comments, :text, false
    change_column_null :ideas, :text, false
  end

  def self.down
    change_column_null :comments, :text, true
    change_column_null :ideas, :text, true
  end
end
