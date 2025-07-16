.PHONY: build test clean container-build container-push deploy undeploy kustomize-build

# Variables
IMAGE_NAME=quay.io/redhat-ai-dev/rosa-namespace-provisioner
IMAGE_TAG=latest
BINARY_NAME=controller
CONTAINER_RUNTIME?=podman
PLATFORM?=linux/amd64

# Build the Go binary
build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o $(BINARY_NAME) main.go

# Run tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html

# Build container image with UBI
container-build:
	$(CONTAINER_RUNTIME) build --platform $(PLATFORM) -t $(IMAGE_NAME):$(IMAGE_TAG) .

# Legacy docker-build for backwards compatibility
docker-build: container-build

# Push container image
container-push:
	$(CONTAINER_RUNTIME) push $(IMAGE_NAME):$(IMAGE_TAG)

# Legacy docker-push for backwards compatibility  
docker-push: container-push

# Build Kustomize manifests (for testing)
kustomize-build:
	kustomize build deploy/

# Deploy to OpenShift/Kubernetes using Kustomize
deploy:
	kustomize build deploy/ | oc apply -f -

# Remove deployment using Kustomize
undeploy:
	kustomize build deploy/ | oc delete -f -

# Run locally (requires kubeconfig)
run:
	TARGET_GROUP_NAME=${TARGET_GROUP_NAME} go run main.go --v=2

# Download dependencies
deps:
	go mod download
	go mod tidy

# Format code
fmt:
	go fmt ./...

# Lint code (optional target)
lint:
	@which golangci-lint > /dev/null || echo "golangci-lint not found, skipping..."
	@which golangci-lint > /dev/null && golangci-lint run || true

# Verify deployment files
verify:
	@echo "Verifying Kustomize configuration..."
	kustomize build deploy/ > /dev/null
	@echo "✓ Kustomize configuration is valid"

# Build, containerize and deploy
all: build container-build deploy

# Development workflow
dev: fmt lint test build

# Help target
help:
	@echo "Available targets:"
	@echo "  build          - Build the Go binary"
	@echo "  test           - Run tests with verbose output"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  clean          - Clean build artifacts"
	@echo "  container-build - Build container image (uses $$CONTAINER_RUNTIME, default: podman)"
	@echo "  container-push - Push container image"
	@echo "  kustomize-build - Build Kustomize manifests for testing"
	@echo "  deploy         - Deploy to cluster using Kustomize"
	@echo "  undeploy       - Remove deployment using Kustomize"
	@echo "  run            - Run locally (set TARGET_GROUP_NAME env var)"
	@echo "  verify         - Verify Kustomize configuration"
	@echo "  dev            - Development workflow (fmt, lint, test, build)"
	@echo "  all            - Build, containerize and deploy"
	@echo ""
	@echo "Environment variables:"
	@echo "  CONTAINER_RUNTIME - Container runtime (default: podman)"
	@echo "  PLATFORM         - Target platform (default: linux/amd64)"
	@echo "  TARGET_GROUP_NAME - Group to watch when running locally" 