class MakeCommentEmailNotNull < ActiveRecord::Migration
  def self.up
    Comment.where(:email => nil).delete_all
    Idea.where(:email => nil).delete_all

    change_column_null :comments, :email, false
    change_column_null :ideas, :email, false
  end

  def self.down
    change_column_null :comments, :email, true
    change_column_null :ideas, :email, true
  end
end
