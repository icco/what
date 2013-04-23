What::App.controllers  do
  get :index do
    @ideas = Idea.all
    render :index
  end

  post :index do
    i = Idea.new
    i.text = params['text']
    i.email = session[:user_email]
    i.save

    redirect '/'
  end

  get :idea, :with => :id do
    @idea = Idea.find(params[:id])
    render :idea
  end

  post :idea, :with => :id do
    @idea = Idea.find(params[:id])
    c = Comment.new
    c.idea = @idea
    c.text = params['text']
    c.email = session[:user_email]
    c.save

    redirect "/idea/#{params[:id]}"
  end

  get :login do
    redirect "/auth/gplus"
  end
  
  get "/auth/gplus/callback" do
    auth_hash = request.env["omniauth.auth"]
    session[:user_email] = auth_hash['email']
    flash[:message] = "You are now signed in as #{session[:user_email]}."

    redirect '/'
  end
end
