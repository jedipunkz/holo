.PHONY: build test perf install update dev-install

build:
	go build -o holo .

test:
	go test -race ./...

perf:
	go test -run '^$$' -bench . -benchmem ./...

install:
	go install .

update:
	go get -u ./...
	go mod tidy

dev-install: build
	mkdir -p $(HOME)/.bin
	cp ./holo $(HOME)/.bin/holo
