default: build

build:
	go build -v ./...

install: build
	go install -v ./...

lint:
	golangci-lint run

generate:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name livck

fmt:
	gofmt -s -w .

test:
	go test -v -race -cover ./internal/...

# Acceptance tests run against a REAL LIVCK instance:
#   TF_ACC=1 LIVCK_ENDPOINT=http://localhost:15800/api LIVCK_API_TOKEN=lvk_... make testacc
# With OpenTofu as the test runner, additionally set:
#   TF_ACC_TERRAFORM_PATH=$(which tofu) TF_ACC_PROVIDER_HOST=registry.opentofu.org
testacc:
	TF_ACC=1 go test -v -timeout 120m ./internal/provider/

.PHONY: default build install lint generate fmt test testacc
