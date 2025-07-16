# Use UBI9 Go toolset for building
FROM registry.access.redhat.com/ubi9/go-toolset:1.24 AS builder

WORKDIR /opt/app-root/src

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY main.go ./
COPY pkg/ pkg/

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o controller main.go

# Final stage using UBI minimal for smaller footprint
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

# Install ca-certificates using microdnf
RUN microdnf update -y && \
    microdnf install -y ca-certificates && \
    microdnf clean all

# Create a non-root user for security
RUN useradd -r -u 1001 -g 0 controller

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /opt/app-root/src/controller .

# Change ownership to the non-root user
RUN chown -R 1001:0 /app && chmod -R g=u /app

# Switch to non-root user
USER 1001

# Run the binary
CMD ["./controller"] 