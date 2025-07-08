.PHONY: fmt lint

fmt:
	go fmt ./...

lint:
	golangci-lint run ./...