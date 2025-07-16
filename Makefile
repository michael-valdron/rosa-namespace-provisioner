.PHONY: build test clean docker-build docker-push deploy undeploy

# Variables
IMAGE_NAME=rosa-namespace-provisioner
IMAGE_TAG=latest
BINARY_NAME=controller

# Build the Go binary
build:
	go build -o $(BINARY_NAME) main.go

# Run tests
test:
	go test ./...

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)

# Build Docker image
docker-build:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

# Push Docker image (configure your registry)
docker-push:
	docker push $(IMAGE_NAME):$(IMAGE_TAG)

# Deploy to OpenShift/Kubernetes
deploy:
	oc apply -f deployment.yaml

# Remove deployment
undeploy:
	oc delete -f deployment.yaml

# Run locally (requires kubeconfig)
run:
	go run main.go --v=2

# Download dependencies
deps:
	go mod download
	go mod tidy

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run

# Build and deploy
all: build docker-build deploy 