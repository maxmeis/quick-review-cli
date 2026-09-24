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

verify: build vet coverage e2e

clean:
	rm -rf bin coverage.out

# CGO-free archives for the supported macOS and Linux platforms.
.PHONY: dist
dist:
	rm -rf dist
	mkdir -p dist
	@set -eu; for os in darwin linux; do \
	  for arch in amd64 arm64; do \
	    name="quick-review-$$os-$$arch"; \
	    mkdir -p "dist/$$name"; \
	    CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -o "dist/$$name/review" ./cmd/review; \
	    tar -czf "dist/$$name.tar.gz" -C "dist/$$name" review; \
	    rm -rf "dist/$$name"; \
	  done; \
	done
	cd dist && shasum -a 256 *.tar.gz > checksums.txt

.PHONY: e2e
e2e:
	go test -race -tags=e2e -count=1 -timeout=180s ./tests/e2e
