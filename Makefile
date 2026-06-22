BINARY := ./bin/toaster

.PHONY: build test vet fmt fmt-check lint check gate clean

build:
	go build -o $(BINARY) ./cmd/toaster

test:
	go test ./...

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
	rm -rf ./bin
