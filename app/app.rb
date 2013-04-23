module What
  class App < Padrino::Application
    register SassInitializer
    use ActiveRecord::ConnectionAdapters::ConnectionManagement
    register Padrino::Rendering
    register Padrino::Mailer
    register Padrino::Helpers

    enable :sessions

    use OmniAuth::Builder do
      provider :google_oauth2, ENV['GPLUS_ID'], ENV['GPLUS_SECRET'], {
        :scope => "userinfo.email,userinfo.profile,plus.me",
        :approval_prompt => "auto"
      }
    end

    layout :main
  end
end
