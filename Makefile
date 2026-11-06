.PHONY: build test vet fmt smoke

build:
	go build -o terraform-provider-onidel .

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w main.go internal
	terraform fmt -recursive examples

smoke: build
	python3 scripts/smoke_test.py
