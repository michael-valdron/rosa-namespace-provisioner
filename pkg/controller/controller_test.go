package controller

import (
	"context"
	"fmt"
	"os"
	"testing"

	projectv1 "github.com/openshift/api/project/v1"
	userv1 "github.com/openshift/api/user/v1"
	projectfake "github.com/openshift/client-go/project/clientset/versioned/fake"
	userfake "github.com/openshift/client-go/user/clientset/versioned/fake"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetTargetGroupName(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     string
	}{
		{
			name:     "environment variable set",
			envValue: "my-custom-group",
			want:     "my-custom-group",
		},
		{
			name:     "environment variable empty",
			envValue: "",
			want:     defaultTargetGroupName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up environment
			originalValue := os.Getenv("TARGET_GROUP_NAME")
			defer func() {
				if originalValue != "" {
					os.Setenv("TARGET_GROUP_NAME", originalValue)
				} else {
					os.Unsetenv("TARGET_GROUP_NAME")
				}
			}()

			if tt.envValue == "" {
				os.Unsetenv("TARGET_GROUP_NAME")
			} else {
				os.Setenv("TARGET_GROUP_NAME", tt.envValue)
			}

			got := GetTargetGroupName()
			if got != tt.want {
				t.Errorf("GetTargetGroupName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestController_createRoleBinding(t *testing.T) {
	tests := []struct {
		name  string
		users []struct {
			user    string
			project string
		}
		existingRoleBindings []struct {
			name    string
			project string
		}
		shouldError bool
	}{
		{
			name: "Create RoleBindings for target users and their projects",
			users: []struct {
				user    string
				project string
			}{
				{
					user:    "bob",
					project: "bob-dev",
				},
				{
					user:    "john",
					project: "ai-dev",
				},
				{
					user:    "sarah",
					project: "workspace",
				},
			},
		},
		{
			name: "Create RoleBindings for target users and their projects along existing RoleBindings",
			users: []struct {
				user    string
				project string
			}{
				{
					user:    "bob",
					project: "bob-dev",
				},
				{
					user:    "john",
					project: "ai-dev",
				},
				{
					user:    "sarah",
					project: "workspace",
				},
			},
			existingRoleBindings: []struct {
				name    string
				project string
			}{
				{
					name:    "ai-dev-edit",
					project: "ai-dev-testing",
				},
				{
					name:    "project-edit",
					project: "dev",
				},
			},
		},
		{
			name: "Attempt to create an existing RoleBinding",
			users: []struct {
				user    string
				project string
			}{
				{
					user:    "john",
					project: "ai-dev",
				},
			},
			existingRoleBindings: []struct {
				name    string
				project string
			}{
				{
					name:    "ai-dev-edit",
					project: "ai-dev",
				},
			},
		},
		{
			name: "Attempt to create two RoleBindings for the same project",
			users: []struct {
				user    string
				project string
			}{
				{
					user:    "john",
					project: "ai-dev",
				},
				{
					user:    "mike",
					project: "ai-dev",
				},
			},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Create addedUsers
			addedUsers := make(map[string]bool)
			for _, userinfo := range tt.users {
				addedUsers[userinfo.user] = false
			}

			// Create addedProjects
			addedProjects := make(map[string]bool)
			for _, userinfo := range tt.users {
				addedProjects[userinfo.project] = false
			}

			// Create addedRoleBindings
			addedRoleBindings := make(map[string]bool)
			for _, roleBinding := range tt.existingRoleBindings {
				addedRoleBindings[roleBinding.name] = false
			}

			// Create fake user objects and their project/namespace objects
			var userObjects []runtime.Object
			var projectObjects []runtime.Object
			var kubernetesObjects []runtime.Object
			for _, userinfo := range tt.users {
				if !addedUsers[userinfo.user] {
					userObjects = append(userObjects, &userv1.User{
						ObjectMeta: metav1.ObjectMeta{
							Name: userinfo.user,
						},
					})
					addedUsers[userinfo.user] = true
				}
				if !addedProjects[userinfo.project] {
					projectObjects = append(projectObjects, &projectv1.Project{
						ObjectMeta: metav1.ObjectMeta{
							Name: userinfo.project,
						},
					})
					kubernetesObjects = append(kubernetesObjects, &corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							Name: userinfo.project,
						},
					})
					addedProjects[userinfo.project] = true
				}
			}

			// Create fake existing RoleBindings objects and their project/namespace objects
			for _, roleBinding := range tt.existingRoleBindings {
				if !addedRoleBindings[roleBinding.name] {
					kubernetesObjects = append(kubernetesObjects, &rbacv1.RoleBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name:      roleBinding.name,
							Namespace: roleBinding.project,
						},
					})
					addedRoleBindings[roleBinding.name] = true
				}
				if !addedProjects[roleBinding.project] {
					projectObjects = append(projectObjects, &projectv1.Project{
						ObjectMeta: metav1.ObjectMeta{
							Name: roleBinding.project,
						},
					})
					kubernetesObjects = append(kubernetesObjects, &corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							Name: roleBinding.project,
						},
					})
					addedProjects[roleBinding.project] = true
				}
			}

			// Create fake clients
			userClient := userfake.NewSimpleClientset(userObjects...)
			projectClient := projectfake.NewSimpleClientset(projectObjects...)
			rbacClient := fake.NewSimpleClientset(kubernetesObjects...).RbacV1()

			// Create controller
			controller := &Controller{
				userClient:    userClient,
				projectClient: projectClient,
				rbacClient:    rbacClient,
			}

			errorCount := 0
			for _, userinfo := range tt.users {
				expectedRoleBindingName := fmt.Sprintf("%s-edit", userinfo.project)
				err := controller.createRoleBinding(userinfo.user, userinfo.project)
				if !tt.shouldError && err != nil {
					t.Errorf("Expected RoleBinding %s to be created, but got error: %v", expectedRoleBindingName, err)
					continue
				} else if tt.shouldError && err != nil {
					errorCount += 1
					continue
				}

				_, err = controller.rbacClient.RoleBindings(userinfo.project).Get(ctx, expectedRoleBindingName, metav1.GetOptions{})
				if err != nil {
					t.Errorf("Expected RoleBinding %s to be found, but got error: %v", expectedRoleBindingName, err)
				}
			}

			if tt.shouldError && errorCount == 0 {
				t.Errorf("Expected case '%s' to receive error(s)", tt.name)
			}
		})
	}
}

func TestController_createUserProject(t *testing.T) {
	tests := []struct {
		name             string
		users            []string
		existingProjects []string
		shouldError      bool
	}{
		{
			name:  "Create projects for target users",
			users: []string{"bob", "john", "sarah"},
		},
		{
			name:             "Create projects for target users along existing projects",
			users:            []string{"bob", "john", "sarah"},
			existingProjects: []string{"bob-dev", "ai-dev", "workspace"},
		},
		{
			name:             "Attempt to create an existing project",
			users:            []string{"bob"},
			existingProjects: []string{"bob"},
		},
		{
			name:             "Attempt to create existing projects",
			users:            []string{"bob", "john"},
			existingProjects: []string{"bob", "john"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Create addedUsers
			addedUsers := make(map[string]bool)
			for _, user := range tt.users {
				addedUsers[user] = false
			}

			// Create addedProjects
			addedProjects := make(map[string]bool)
			for _, project := range tt.existingProjects {
				addedProjects[project] = false
			}

			// Create fake user objects and their project/namespace objects
			var userObjects []runtime.Object
			for _, user := range tt.users {
				if !addedUsers[user] {
					userObjects = append(userObjects, &userv1.User{
						ObjectMeta: metav1.ObjectMeta{
							Name: user,
						},
					})
					addedUsers[user] = true
				}
			}

			// Create fake existing project/namespace objects
			var projectObjects []runtime.Object
			var namespaceObjects []runtime.Object
			for _, project := range tt.existingProjects {
				if !addedProjects[project] {
					projectObjects = append(projectObjects, &projectv1.Project{
						ObjectMeta: metav1.ObjectMeta{
							Name: project,
						},
					})
					namespaceObjects = append(namespaceObjects, &corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							Name: project,
						},
					})
					addedProjects[project] = true
				}
			}

			// Create fake clients
			userClient := userfake.NewSimpleClientset(userObjects...)
			projectClient := projectfake.NewSimpleClientset(projectObjects...)
			rbacClient := fake.NewSimpleClientset(namespaceObjects...).RbacV1()

			// Create controller
			controller := &Controller{
				userClient:    userClient,
				projectClient: projectClient,
				rbacClient:    rbacClient,
			}

			errorCount := 0
			for _, user := range tt.users {
				err := controller.createUserProject(user)
				if !tt.shouldError && err != nil {
					t.Errorf("Expected project %s to be created, but got error: %v", user, err)
					continue
				} else if tt.shouldError && err != nil {
					errorCount += 1
					continue
				}

				_, err = controller.projectClient.ProjectV1().Projects().Get(ctx, user, metav1.GetOptions{})
				if err != nil {
					t.Errorf("Expected project %s to be found, but got error: %v", user, err)
				}
			}

			if tt.shouldError && errorCount == 0 {
				t.Errorf("Expected case '%s' to receive error(s)", tt.name)
			}
		})
	}
}

func TestController_handleGroup(t *testing.T) {
	tests := []struct {
		name             string
		oldGroup         *userv1.Group
		newGroup         *userv1.Group
		existingProjects []string
		expectedCreated  []string
		expectedDeleted  []string
		shouldError      bool
	}{
		{
			name:     "group creation with users",
			oldGroup: nil,
			newGroup: &userv1.Group{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-group",
				},
				Users: []string{"alice", "bob"},
			},
			existingProjects: []string{},
			expectedCreated:  []string{"alice", "bob"},
			expectedDeleted:  []string{},
		},
		{
			name:     "group creation empty",
			oldGroup: nil,
			newGroup: &userv1.Group{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-group",
				},
				Users: []string{},
			},
			existingProjects: []string{},
			expectedCreated:  []string{},
			expectedDeleted:  []string{},
		},
		{
			name: "user added to group",
			oldGroup: &userv1.Group{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-group",
					ResourceVersion: "1",
				},
				Users: []string{"alice"},
			},
			newGroup: &userv1.Group{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-group",
					ResourceVersion: "2",
				},
				Users: []string{"alice", "bob"},
			},
			existingProjects: []string{},
			expectedCreated:  []string{"bob"},
			expectedDeleted:  []string{},
		},
		{
			name: "user removed from group",
			oldGroup: &userv1.Group{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-group",
					ResourceVersion: "1",
				},
				Users: []string{"alice", "bob"},
			},
			newGroup: &userv1.Group{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-group",
					ResourceVersion: "2",
				},
				Users: []string{"alice"},
			},
			existingProjects: []string{"bob"},
			expectedCreated:  []string{},
			expectedDeleted:  []string{"bob"},
		},
		{
			name: "users added and removed",
			oldGroup: &userv1.Group{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-group",
					ResourceVersion: "1",
				},
				Users: []string{"alice", "charlie"},
			},
			newGroup: &userv1.Group{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-group",
					ResourceVersion: "2",
				},
				Users: []string{"alice", "bob"},
			},
			existingProjects: []string{"charlie"},
			expectedCreated:  []string{"bob"},
			expectedDeleted:  []string{"charlie"},
		},
		{
			name: "no changes",
			oldGroup: &userv1.Group{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-group",
					ResourceVersion: "1",
				},
				Users: []string{"alice"},
			},
			newGroup: &userv1.Group{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-group",
					ResourceVersion: "2",
				},
				Users: []string{"alice"},
			},
			existingProjects: []string{},
			expectedCreated:  []string{},
			expectedDeleted:  []string{},
		},
		{
			name:     "project already exists",
			oldGroup: nil,
			newGroup: &userv1.Group{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-group",
				},
				Users: []string{"alice"},
			},
			existingProjects: []string{"alice"},
			expectedCreated:  []string{}, // Won't create because it already exists
			expectedDeleted:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Create fake clients
			var projectObjects []runtime.Object
			var namespaceObjects []runtime.Object
			for _, projectName := range tt.existingProjects {
				projectObjects = append(projectObjects, &projectv1.Project{
					ObjectMeta: metav1.ObjectMeta{
						Name: projectName,
					},
				})
				namespaceObjects = append(namespaceObjects, &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: projectName,
					},
				})
			}

			userClient := userfake.NewSimpleClientset()
			projectClient := projectfake.NewSimpleClientset(projectObjects...)
			rbacClient := fake.NewSimpleClientset(namespaceObjects...).RbacV1()

			// Create controller
			controller := &Controller{
				userClient:    userClient,
				projectClient: projectClient,
				rbacClient:    rbacClient,
			}

			// Call handleGroup
			controller.handleGroup(tt.oldGroup, tt.newGroup)

			// Verify created projects
			for _, expectedProject := range tt.expectedCreated {
				_, err := projectClient.ProjectV1().Projects().Get(ctx, expectedProject, metav1.GetOptions{})
				if err != nil {
					t.Errorf("Expected project %s to be created, but got error: %v", expectedProject, err)
				}
			}

			// Verify deleted projects
			for _, expectedDeleted := range tt.expectedDeleted {
				_, err := projectClient.ProjectV1().Projects().Get(ctx, expectedDeleted, metav1.GetOptions{})
				if !errors.IsNotFound(err) {
					t.Errorf("Expected project %s to be deleted, but it still exists", expectedDeleted)
				}
			}

			// Verify projects that should still exist
			allProjects, err := projectClient.ProjectV1().Projects().List(ctx, metav1.ListOptions{})
			if err != nil {
				t.Fatalf("Failed to list projects: %v", err)
			}

			// Count expected projects after operations
			expectedProjects := make(map[string]bool)

			// Start with existing projects
			for _, proj := range tt.existingProjects {
				expectedProjects[proj] = true
			}

			// Add created projects
			for _, proj := range tt.expectedCreated {
				expectedProjects[proj] = true
			}

			// Remove deleted projects
			for _, proj := range tt.expectedDeleted {
				delete(expectedProjects, proj)
			}

			if len(allProjects.Items) != len(expectedProjects) {
				t.Errorf("Expected %d projects after operations, but got %d", len(expectedProjects), len(allProjects.Items))
			}

			// Verify each remaining project is expected
			for _, project := range allProjects.Items {
				if !expectedProjects[project.Name] {
					t.Errorf("Unexpected project %s found after operations", project.Name)
				}
			}
		})
	}
}

func TestController_handleGroupErrorHandling(t *testing.T) {
	t.Run("should continue processing when project creation fails for one user", func(t *testing.T) {
		ctx := context.Background()

		// Pre-create a project to cause a conflict
		existingProject := &projectv1.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name: "alice",
			},
		}
		existingNamespace := &corev1.Namespace{ObjectMeta: existingProject.ObjectMeta}

		userClient := userfake.NewSimpleClientset()
		projectClient := projectfake.NewSimpleClientset(existingProject)
		rbacClient := fake.NewSimpleClientset(existingNamespace).RbacV1()

		controller := &Controller{
			userClient:    userClient,
			projectClient: projectClient,
			rbacClient:    rbacClient,
		}

		// Create group with users where one will conflict
		newGroup := &userv1.Group{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-group",
			},
			Users: []string{"alice", "bob"}, // alice already exists, bob is new
		}

		// Call handleGroup (group creation)
		controller.handleGroup(nil, newGroup)

		// Verify alice project still exists (creation should have been skipped due to conflict)
		_, err := projectClient.ProjectV1().Projects().Get(ctx, "alice", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Expected alice project to still exist, but got error: %v", err)
		}

		// Verify bob project was created despite alice conflict
		_, err = projectClient.ProjectV1().Projects().Get(ctx, "bob", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Expected bob project to be created despite alice conflict, but got error: %v", err)
		}
	})
}

func TestNewController(t *testing.T) {
	// Set up environment
	originalValue := os.Getenv("TARGET_GROUP_NAME")
	defer func() {
		if originalValue != "" {
			os.Setenv("TARGET_GROUP_NAME", originalValue)
		} else {
			os.Unsetenv("TARGET_GROUP_NAME")
		}
	}()

	os.Setenv("TARGET_GROUP_NAME", "test-group")

	userClient := userfake.NewSimpleClientset()
	projectClient := projectfake.NewSimpleClientset()
	rbacClient := fake.NewSimpleClientset().RbacV1()

	controller := NewController(userClient, projectClient, rbacClient)

	if controller == nil {
		t.Fatal("Expected controller to be created, but got nil")
	}

	if controller.userClient != userClient {
		t.Error("Expected userClient to be set correctly")
	}

	if controller.projectClient != projectClient {
		t.Error("Expected projectClient to be set correctly")
	}

	if controller.rbacClient != rbacClient {
		t.Error("Expected rbacClient to be set correctly")
	}

	if controller.informer == nil {
		t.Error("Expected informer to be created")
	}

	if controller.stopCh == nil {
		t.Error("Expected stopCh to be created")
	}
}
