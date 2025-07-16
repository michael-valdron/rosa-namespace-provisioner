package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	projectv1 "github.com/openshift/api/project/v1"
	userv1 "github.com/openshift/api/user/v1"
	projectclient "github.com/openshift/client-go/project/clientset/versioned"
	userclient "github.com/openshift/client-go/user/clientset/versioned"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

const (
	// The specific group name to watch for
	targetGroupName = "redhat-ai-dev-edit-users"
)

type Controller struct {
	userClient    userclient.Interface
	projectClient projectclient.Interface
	informer      cache.SharedIndexInformer
	stopCh        chan struct{}
}

func NewController(userClient userclient.Interface, projectClient projectclient.Interface) *Controller {
	// Create a filtered informer that only watches our specific group
	listWatcher := cache.NewListWatchFromClient(
		userClient.UserV1().RESTClient(),
		"groups",
		"", // namespace - empty for cluster-scoped resources
		fields.OneTermEqualSelector("metadata.name", targetGroupName),
	)

	// Create informer with only the specific group
	informer := cache.NewSharedIndexInformer(
		listWatcher,
		&userv1.Group{},
		time.Minute*10, // resync period
		cache.Indexers{},
	)

	controller := &Controller{
		userClient:    userClient,
		projectClient: projectClient,
		informer:      informer,
		stopCh:        make(chan struct{}),
	}

	// Add event handlers
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			// We only care about updates, so we'll ignore Add events
			group := obj.(*userv1.Group)
			klog.V(4).Infof("Group %s was added (ignoring)", group.Name)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			// This is the main event we're interested in
			controller.handleGroupUpdate(oldObj.(*userv1.Group), newObj.(*userv1.Group))
		},
		DeleteFunc: func(obj interface{}) {
			// We only care about updates, so we'll ignore Delete events
			group := obj.(*userv1.Group)
			klog.V(4).Infof("Group %s was deleted (ignoring)", group.Name)
		},
	})

	return controller
}

func (c *Controller) handleGroupUpdate(oldGroup, newGroup *userv1.Group) {
	// Only process if this is the specific group we're watching
	if newGroup.Name != targetGroupName {
		return
	}

	klog.Infof("Detected update to Group: %s", newGroup.Name)

	// Log the changes for debugging purposes
	klog.V(2).Infof("Old Group ResourceVersion: %s", oldGroup.ResourceVersion)
	klog.V(2).Infof("New Group ResourceVersion: %s", newGroup.ResourceVersion)

	// Check if users were added or removed
	oldUsers := make(map[string]bool)
	for _, user := range oldGroup.Users {
		oldUsers[user] = true
	}

	newUsers := make(map[string]bool)
	for _, user := range newGroup.Users {
		newUsers[user] = true
	}

	// Find added users
	var addedUsers []string
	for user := range newUsers {
		if !oldUsers[user] {
			addedUsers = append(addedUsers, user)
		}
	}

	// Find removed users
	var removedUsers []string
	for user := range oldUsers {
		if !newUsers[user] {
			removedUsers = append(removedUsers, user)
		}
	}

	if len(addedUsers) > 0 {
		klog.Infof("Users added to group %s: %v", newGroup.Name, addedUsers)

		// For each added user, check if a project exists with the same name as the user
		for _, user := range addedUsers {
			// Check if a project exists with the same name as the user
			project, err := c.projectClient.ProjectV1().Projects().Get(context.Background(), user, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					klog.Infof("Project %s not found for user %s", user, user)
					// TODO: Create project logic could go here
					project := &projectv1.Project{
						ObjectMeta: metav1.ObjectMeta{
							Name: user,
						},
					}
					_, err = c.projectClient.ProjectV1().Projects().Create(context.Background(), project, metav1.CreateOptions{})
					if err != nil {
						klog.Errorf("Error creating project for user %s: %v", user, err)
					}
				} else {
					// Just log the error for now
					klog.Errorf("Error checking if project exists for user %s: %v", user, err)
				}
			} else {
				klog.Infof("Project %s already exists for user %s", project.Name, user)
			}
		}
	}

	if len(removedUsers) > 0 {
		klog.Infof("Users removed from group %s: %v", newGroup.Name, removedUsers)
		// TODO: Add your custom logic here for handling removed users
		for _, user := range removedUsers {
			// Check if a project exists with the same name as the user
			project, err := c.projectClient.ProjectV1().Projects().Get(context.Background(), user, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("Error checking if project exists for user %s: %v", user, err)
			}
			if project != nil {
				// Delete the project
				err = c.projectClient.ProjectV1().Projects().Delete(context.Background(), user, metav1.DeleteOptions{})
				if err != nil {
					klog.Errorf("Error deleting project for user %s: %v", user, err)
				}
			} else {
				klog.Infof("Project %s does not exist for user %s", user, user)
			}
		}
	}

	if len(addedUsers) == 0 && len(removedUsers) == 0 {
		klog.V(2).Infof("Group %s was updated but no user changes detected", newGroup.Name)
	}
}

func (c *Controller) Run(ctx context.Context) error {
	klog.Info("Starting controller")

	// Start the informer
	go c.informer.Run(c.stopCh)

	// Wait for the informer cache to sync
	if !cache.WaitForCacheSync(c.stopCh, c.informer.HasSynced) {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Infof("Controller started successfully, watching for updates to Group: %s", targetGroupName)

	// Wait for context cancellation
	<-ctx.Done()

	klog.Info("Shutting down controller")
	close(c.stopCh)

	return nil
}

func main() {
	klog.InitFlags(nil)

	// Build the Kubernetes client configuration
	var config *rest.Config
	var err error

	// Try to get in-cluster config first
	config, err = rest.InClusterConfig()
	if err != nil {
		// If not in cluster, try to get kubeconfig
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = os.Getenv("HOME") + "/.kube/config"
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			klog.Fatalf("Failed to build config: %v", err)
		}
	}

	// Create the OpenShift user client
	userClient, err := userclient.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create OpenShift user client: %v", err)
	}

	// Create the OpenShift project client
	projectClient, err := projectclient.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create OpenShift project client: %v", err)
	}

	// Create and start the controller
	controller := NewController(userClient, projectClient)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		klog.Info("Received shutdown signal")
		cancel()
	}()

	if err := controller.Run(ctx); err != nil {
		klog.Fatalf("Controller failed: %v", err)
	}

	klog.Info("Controller shut down gracefully")
}
