module What
  class App < Padrino::Application
    register SassInitializer
    use ActiveRecord::ConnectionAdapters::ConnectionManagement
    register Padrino::Rendering
    register Padrino::Mailer
    register Padrino::Helpers

    enable :sessions

    use OmniAuth::Builder do
      provider :gplus, ENV['GPLUS_ID'], ENV['GPLUS_SECRET'], :scope => 'plus.me'
    end

    layout :main
  end
end
