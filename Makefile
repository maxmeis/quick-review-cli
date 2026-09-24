.PHONY: build test coverage vet verify clean

build:
	mkdir -p bin
	go build -o bin/review ./cmd/review

test:
	go test -race ./...

coverage:
	go test -race -covermode=atomic -coverprofile=coverage.out ./...
	awk 'NR == 1 { next } { if ($$2 > 0) { blocks += $$2; if ($$3 == 0) { print "uncovered statement block: " $$1; failed = 1 } } } END { if (blocks == 0) { print "coverage profile contains no statement blocks"; exit 1 } if (failed) exit 1 }' coverage.out
	go tool cover -func=coverage.out

vet:
	go vet ./...

verify: build vet coverage

clean:
	rm -rf bin coverage.out
