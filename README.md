# ROSA Namespace Provisioner

A Kubernetes controller that watches for updates to a specific OpenShift Group resource and automatically manages OpenShift projects (namespaces) for users in that group.

## Features

- Watches a configurable OpenShift Group resource (default: `redhat-ai-dev-edit-users`)
- Automatically creates OpenShift projects when users are added to the group
- Automatically deletes OpenShift projects when users are removed from the group
- Project names match the username for easy identification
- Only responds to Update events (ignores Add and Delete events)
- Configurable via environment variables
- Graceful shutdown handling
- Built on Red Hat UBI (Universal Base Image) for OpenShift compatibility

## Prerequisites

- OpenShift or ROSA cluster access
- Go 1.23 or later
- Docker or Podman (for containerized deployment)
- Cluster admin permissions (for RBAC setup)

## Building

### Local Build
```bash
make build
```

### Container Build
```bash
make docker-build
```

The container uses Red Hat UBI9 base images for optimal OpenShift compatibility.

## Configuration

### Environment Variables

- `TARGET_GROUP_NAME`: The OpenShift group to watch (default: `redhat-ai-dev-edit-users`)

### Example
```bash
export TARGET_GROUP_NAME="my-custom-group"
```

## Deployment

The deployment is organized using Kustomize for better resource management:

```
deploy/
├── deployment.yaml      # Controller deployment
├── serviceaccount.yaml  # Service account
├── rbac.yaml           # RBAC permissions
└── kustomization.yaml  # Kustomize configuration
```

### Deploy to OpenShift
```bash
# Deploy using kustomize
kustomize build deploy/ | oc apply -f -

# Or using kubectl with kustomize
kubectl apply -k deploy/

# Or using oc with kustomize
oc apply -k deploy/
```

### Deploy with Custom Configuration
```bash
# Create a custom kustomization for your environment
cat <<EOF > my-kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: my-namespace

resources:
- deploy/

patchesStrategicMerge:
- |-
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: rosa-namespace-provisioner
  spec:
    template:
      spec:
        containers:
        - name: controller
          env:
          - name: TARGET_GROUP_NAME
            value: "my-custom-group"
EOF

kustomize build . | oc apply -f -
```

## Permissions

The controller requires the following RBAC permissions:

### Groups (user.openshift.io)
- `get`, `list`, `watch` on `groups` resources

### Projects (project.openshift.io)  
- `get`, `list`, `create`, `delete` on `projects` resources

These permissions are automatically configured when you deploy using the provided RBAC manifests.

## Running Locally

### Development
```bash
# Ensure you have a valid kubeconfig
export KUBECONFIG=~/.kube/config

# Optionally configure the target group
export TARGET_GROUP_NAME="my-test-group"

# Run the controller
make run
```

## Logging

The controller uses klog for logging. Set the verbosity level using the `-v` flag:
- `-v=0`: Basic info messages
- `-v=2`: Detailed change information and project operations
- `-v=4`: Debug messages including ignored events

## How It Works

1. **Group Monitoring**: The controller creates a filtered informer that only watches the specified OpenShift group
2. **Change Detection**: On group updates, it compares old and new user lists to identify additions and removals
3. **Project Management**: 
   - **User Added**: Creates an OpenShift project with the same name as the username
   - **User Removed**: Deletes the OpenShift project with the same name as the username
4. **Error Handling**: Logs errors but continues processing other users if individual operations fail

## Example Workflow

1. User `alice` is added to the watched group
2. Controller detects the change and creates project `alice`
3. User `alice` can now use the `alice` project/namespace
4. User `alice` is removed from the group
5. Controller detects the change and deletes project `alice`

## Development

### Dependencies
```bash
make deps
```

### Formatting
```bash
make fmt
```

### Testing
```bash
make test
```

### Cleanup
```bash
make clean
```

## Architecture

The controller uses the OpenShift client-go library to:
1. Create a filtered informer that only watches the specific group
2. Handle Update events to detect user changes
3. Compare old and new user lists to identify additions and removals
4. Manage OpenShift project lifecycle automatically
5. Provide detailed logging for operations and troubleshooting

## Customization

The core project management logic is implemented in the `handleGroupUpdate` function in `main.go`. You can extend this function to add additional logic such as:

- Setting project quotas or limits
- Adding additional role bindings
- Configuring network policies
- Setting up monitoring or logging

## Troubleshooting

### Common Issues

1. **Permission Denied**: Ensure the service account has proper RBAC permissions for both groups and projects
2. **Group Not Found**: Verify the group name exists and the `TARGET_GROUP_NAME` environment variable is set correctly
3. **Project Creation Fails**: Check cluster resource quotas and ensure the controller has project creation permissions

### Debug Logging
```bash
# Run with debug logging
oc logs -f deployment/rosa-namespace-provisioner -c controller | grep -v "level=4"
```
