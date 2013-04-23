What::App.controllers  do
  before do
    @logged_in = !session[:user_email].nil?
  end

  get :index do
    @ideas = Idea.all
    render :index
  end

  post :index do
    i = Idea.new
    i.text = params['text']
    i.email = session[:user_email]
    i.save if @logged_in

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
    c.save if @logged_in

    redirect "/idea/#{params[:id]}"
  end

  get :login do
    redirect "/auth/google_oauth2"
  end

  get :logout do
    session[:user_email] = nil
    session[:user_image] = nil
    session[:user_creds] = nil
    session.clear

    redirect '/'
  end

  get "/auth/google_oauth2/callback" do
    auth_hash = request.env["omniauth.auth"]
    session[:user_email] = auth_hash.info['email']
    session[:user_image] = auth_hash.info['image']
    session[:user_creds] = auth_hash.credentials

    redirect '/'
  end
end
