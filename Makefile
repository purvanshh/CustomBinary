.PHONY: all build test clean

all: build

build:
	go build -o bserve ./cmd/bserve
	go build -o bcurl ./cmd/bcurl

test:
	go test ./...

clean:
	rm -f bserve bcurl
