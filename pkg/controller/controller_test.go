package controller

import (
	"context"
	"os"
	"testing"

	projectv1 "github.com/openshift/api/project/v1"
	userv1 "github.com/openshift/api/user/v1"
	projectfake "github.com/openshift/client-go/project/clientset/versioned/fake"
	userfake "github.com/openshift/client-go/user/clientset/versioned/fake"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
			want:     "redhat-ai-dev-edit-users",
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
			for _, projectName := range tt.existingProjects {
				projectObjects = append(projectObjects, &projectv1.Project{
					ObjectMeta: metav1.ObjectMeta{
						Name: projectName,
					},
				})
			}

			userClient := userfake.NewSimpleClientset()
			projectClient := projectfake.NewSimpleClientset(projectObjects...)

			// Create controller
			controller := &Controller{
				userClient:    userClient,
				projectClient: projectClient,
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

		userClient := userfake.NewSimpleClientset()
		projectClient := projectfake.NewSimpleClientset(existingProject)

		controller := &Controller{
			userClient:    userClient,
			projectClient: projectClient,
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

	controller := NewController(userClient, projectClient)

	if controller == nil {
		t.Fatal("Expected controller to be created, but got nil")
	}

	if controller.userClient != userClient {
		t.Error("Expected userClient to be set correctly")
	}

	if controller.projectClient != projectClient {
		t.Error("Expected projectClient to be set correctly")
	}

	if controller.informer == nil {
		t.Error("Expected informer to be created")
	}

	if controller.stopCh == nil {
		t.Error("Expected stopCh to be created")
	}
}
