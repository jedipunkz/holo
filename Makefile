.PHONY: build test install update dev-install

build:
	go build -o holo .

test:
	go test -race ./...

install:
	go install .

update:
	go get -u ./...
	go mod tidy

dev-install: build
	mkdir -p $(HOME)/.bin
	cp ./holo $(HOME)/.bin/holo
