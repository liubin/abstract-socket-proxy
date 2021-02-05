.DEFAULT_GOAL := build

all: build test

build:
	go build --mod=vendor -o bin/abstract-socket-proxy
test:
	go test -v --mod=vendor ./...

clean:
	rm -rf ./bin