# ROSA Namespace Provisioner

A Kubernetes controller that watches for updates to a specific OpenShift Group resource (`redhat-ai-dev-edit-users`) and tracks when users are added or removed from the group.

## Features

- Watches a single OpenShift Group resource: `redhat-ai-dev-edit-users`
- Only responds to Update events (ignores Add and Delete events)
- Tracks user additions and removals from the group
- Provides detailed logging of changes
- Graceful shutdown handling

## Prerequisites

- OpenShift or ROSA cluster access
- Go 1.23 or later
- Docker (for containerized deployment)

## Building

### Local Build
```bash
make build
```

### Docker Build
```bash
make docker-build
```

## Running

### Local Development
```bash
# Ensure you have a valid kubeconfig
export KUBECONFIG=~/.kube/config

# Run the controller
make run
```

### Deployment to OpenShift
```bash
# Deploy to your OpenShift cluster
make deploy
```

## Configuration

The controller is configured to watch the group `redhat-ai-dev-edit-users`. To watch a different group, modify the `targetGroupName` constant in `main.go`.

## Permissions

The controller requires the following RBAC permissions:
- `get`, `list`, `watch` on `groups` resources in the `user.openshift.io` API group

These permissions are defined in the `deployment.yaml` file.

## Logging

The controller uses klog for logging. Set the verbosity level using the `-v` flag:
- `-v=0`: Basic info messages
- `-v=2`: Detailed change information
- `-v=4`: Debug messages including ignored events

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
4. Log changes and provide hooks for custom logic

## Customization

To add custom logic when users are added or removed, modify the `handleGroupUpdate` function in `main.go`. Look for the TODO comments:

```go
if len(addedUsers) > 0 {
    klog.Infof("Users added to group %s: %v", newGroup.Name, addedUsers)
    // TODO: Add your custom logic here for handling added users
}

if len(removedUsers) > 0 {
    klog.Infof("Users removed from group %s: %v", newGroup.Name, removedUsers)
    // TODO: Add your custom logic here for handling removed users
}
```
