package controller

import (
	"context"
	"fmt"
	"os"
	"time"

	projectv1 "github.com/openshift/api/project/v1"
	userv1 "github.com/openshift/api/user/v1"
	projectclient "github.com/openshift/client-go/project/clientset/versioned"
	userclient "github.com/openshift/client-go/user/clientset/versioned"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	rbacv1client "k8s.io/client-go/kubernetes/typed/rbac/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// default value of the target group name
const defaultTargetGroupName = "redhat-ai-dev-users"

// GetTargetGroupName returns the target group name from environment variable or default
func GetTargetGroupName() string {
	groupName := os.Getenv("TARGET_GROUP_NAME")
	if groupName == "" {
		return defaultTargetGroupName // default value
	}
	return groupName
}

// Controller represents the OpenShift Group controller that manages project lifecycle
type Controller struct {
	userClient    userclient.Interface
	projectClient projectclient.Interface
	rbacClient    rbacv1client.RbacV1Interface
	informer      cache.SharedIndexInformer
	stopCh        chan struct{}
}

// NewController creates a new Controller instance
func NewController(userClient userclient.Interface, projectClient projectclient.Interface, rbacClient rbacv1client.RbacV1Interface) *Controller {
	// Get the target group name
	targetGroupName := GetTargetGroupName()

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
		rbacClient:    rbacClient,
		informer:      informer,
		stopCh:        make(chan struct{}),
	}

	// Add event handlers
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			// Handle group creation - treat all users as new additions
			group := obj.(*userv1.Group)
			controller.handleGroup(nil, group)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			// This is the main event we're interested in
			controller.handleGroup(oldObj.(*userv1.Group), newObj.(*userv1.Group))
		},
		DeleteFunc: func(obj interface{}) {
			// We don't care about deletes for right now, so we'll ignore Delete events
			group := obj.(*userv1.Group)
			klog.V(4).Infof("Group %s was deleted (ignoring)", group.Name)
		},
	})

	return controller
}

func (c *Controller) handleGroup(oldGroup, newGroup *userv1.Group) {
	if oldGroup == nil {
		klog.Infof("Detected creation of Group: %s", newGroup.Name)
	} else {
		klog.Infof("Detected update to Group: %s", newGroup.Name)
	}

	// Log the changes for debugging purposes
	if oldGroup != nil {
		klog.V(2).Infof("Old Group ResourceVersion: %s", oldGroup.ResourceVersion)
	}
	klog.V(2).Infof("New Group ResourceVersion: %s", newGroup.ResourceVersion)

	// Check if users were added or removed
	oldUsers := make(map[string]bool)
	if oldGroup != nil {
		for _, user := range oldGroup.Users {
			oldUsers[user] = true
		}
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
			roleBindingName := fmt.Sprintf("%s-edit", user)

			// Check if a project exists with the same name as the user
			_, err := c.projectClient.ProjectV1().Projects().Get(context.Background(), user, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					klog.Infof("Project %s not found for user %s", user, user)
					_ = c.createUserProject(user)
				} else {
					// Just log the error for now
					klog.Errorf("Error checking if project exists for user %s: %v", user, err)
				}
			} else {
				klog.Infof("Project %s already exists for user %s", user, user)

				_, err := c.rbacClient.RoleBindings(user).Get(context.Background(), roleBindingName, metav1.GetOptions{})
				if err != nil {
					if errors.IsNotFound(err) {
						klog.Infof("RoleBinding %s not found for user %s under project %s", roleBindingName, user, user)
						_ = c.createRoleBinding(user, user)
					} else {
						klog.Errorf("Error checking if RoleBinding exists for user %s under project %s: %v", user, user, err)
					}
				} else {
					klog.Infof("RoleBinding %s under project %s already exist for user %s", roleBindingName, user, user)
				}
			}
		}
	}

	if len(removedUsers) > 0 {
		klog.Infof("Users removed from group %s: %v", newGroup.Name, removedUsers)
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
		klog.V(2).Infof("Group %s processed but no users to create projects for", newGroup.Name)
	}
}

// Creates Project for target user
func (c *Controller) createUserProject(user string) error {
	project := &projectv1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name: user,
		},
	}
	_, err := c.projectClient.ProjectV1().Projects().Create(context.Background(), project, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("Error creating project for user %s: %v", user, err)
		return err
	} else {
		klog.Infof("Successfully created project %s for user %s", user, user)

		return c.createRoleBinding(user, user)
	}
}

// Creates user project RoleBinding for edit permissions
func (c *Controller) createRoleBinding(user string, projectName string) error {
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-edit", projectName),
			Namespace: projectName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     "User",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     user,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "edit",
		},
	}

	_, err := c.rbacClient.RoleBindings(projectName).Create(context.Background(), roleBinding, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("Error creating edit RoleBinding for user %s under project %s: %v", user, projectName, err)
		return err
	} else {
		klog.Infof("Successfully created edit RoleBinding %s for user %s under project %s", roleBinding.Name, user, projectName)
		return nil
	}
}

// Run starts the controller and blocks until the context is cancelled
func (c *Controller) Run(ctx context.Context) error {
	klog.Info("Starting controller")

	// Start the informer
	go c.informer.Run(c.stopCh)

	// Wait for the informer cache to sync
	if !cache.WaitForCacheSync(c.stopCh, c.informer.HasSynced) {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	targetGroupName := GetTargetGroupName()
	klog.Infof("Controller started successfully, watching for updates to Group: %s", targetGroupName)

	// Wait for context cancellation
	<-ctx.Done()

	klog.Info("Shutting down controller")
	close(c.stopCh)

	return nil
}
