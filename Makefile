BINARY := ./bin/toaster
COVERAGE := coverage.out

.PHONY: build run test coverage vet fmt fmt-check lint check gate clean

build:
	go build -o $(BINARY) ./cmd/toaster

# Clone -> see it work, in one command: score this repo with this repo.
run: build
	$(BINARY) check .

test:
	go test ./...

# Reported, not gated. A number that has to go up forever gets satisfied with
# tests nobody reads; this exists to show what isn't exercised.
coverage:
	go test -coverprofile=$(COVERAGE) ./...
	@go tool cover -func=$(COVERAGE) | tail -1

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; skipping"

# Run before committing: format check + vet + tests.
check: fmt-check vet test

# Dogfood: toaster-ready scores itself and must pass its own ramp-up floor.
gate: build
	$(BINARY) gate .

clean:
	rm -rf ./bin $(COVERAGE)
