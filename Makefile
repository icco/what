.PHONY: run deploy

all: run

css:
	scss --trace -t compressed public/scss/style.scss public/css/style.css

run: css *.go
	gcloud preview app run . --project=natwelch-what

deploy:
	gcloud preview app deploy . --project=natwelch-what

update:
	cd $(GOPATH)/src/github.com/icco/what/; git pull
	goapp get -v -u ...
