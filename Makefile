HOSTNAME=registry.terraform.io
NAMESPACE=agentctx
NAME=agentctx
BINARY=terraform-provider-${NAME}
VERSION=0.1.0
OS_ARCH=$(shell go env GOOS)_$(shell go env GOARCH)

default: build

build:
	go build -o ${BINARY}

install: build
	mkdir -p ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}
	cp ${BINARY} ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}/

test:
	go test ./... -v

testacc:
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m

vet:
	go vet ./...

fmt:
	gofmt -s -w .

lint: vet fmt

release:
	@if [ -z "$(V)" ]; then echo "Usage: make release V=0.2.0"; exit 1; fi
	@git diff --quiet || (echo "Error: working tree is dirty" && exit 1)
	git tag v$(V)
	git push origin v$(V)

clean:
	rm -f ${BINARY}

.PHONY: build install test testacc vet fmt lint release clean
