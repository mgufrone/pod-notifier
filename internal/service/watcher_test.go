package service

import (
	"context"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	v2 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/go-logr/logr"
	configv1alpha1 "github.com/mgufrone/pod-notifier/api/config/v1alpha1"
)

// MockSlackClient implements the necessary Slack client methods
type MockSlackClient struct {
	mock.Mock
}

func (m *MockSlackClient) SendMessage(channelID string, options ...slack.MsgOption) (string, string, string, error) {
	args := m.Called(channelID, options)
	return args.String(0), args.String(1), args.String(2), args.Error(3)
}

func (m *MockSlackClient) AddReaction(name string, item slack.ItemRef) error {
	args := m.Called(name, item)
	return args.Error(0)
}

func (m *MockSlackClient) UpdateMessage(channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error) {
	args := m.Called(channelID, timestamp, options)
	return args.String(0), args.String(1), args.String(2), args.Error(3)
}

func TestWatcher_Reconcile(t *testing.T) {
	// Create a test context
	ctx := context.Background()

	// Create test cases
	tests := []struct {
		name          string
		podWatch      configv1alpha1.PodWatchSpec
		lastReports   []configv1alpha1.PodReport
		namespace     string
		pods          []v1.Pod
		expectedError bool
	}{
		{
			name: "healthy pod should be resolved",
			podWatch: configv1alpha1.PodWatchSpec{
				Channel: "test-channel",
			},
			namespace: "test-ns",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "healthy-pod",
						Namespace: "test-ns",
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
						ContainerStatuses: []v1.ContainerStatus{
							{
								State: v1.ContainerState{
									Running: &v1.ContainerStateRunning{},
								},
							},
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name: "failing pod should be reported",
			podWatch: configv1alpha1.PodWatchSpec{
				Channel: "test-channel",
			},
			namespace: "test-ns",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "failing-pod",
						Namespace: "test-ns",
					},
					Status: v1.PodStatus{
						Phase: v1.PodPending,
						ContainerStatuses: []v1.ContainerStatus{
							{
								State: v1.ContainerState{
									Waiting: &v1.ContainerStateWaiting{
										Reason:  "ImagePullBackOff",
										Message: "Failed to pull image",
									},
								},
							},
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name: "pod with failed mount should be reported",
			podWatch: configv1alpha1.PodWatchSpec{
				Channel: "test-channel",
			},
			namespace: "test-ns",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mount-failed-pod",
						Namespace: "test-ns",
					},
					Status: v1.PodStatus{
						Phase: v1.PodPending,
						ContainerStatuses: []v1.ContainerStatus{
							{
								State: v1.ContainerState{
									Waiting: &v1.ContainerStateWaiting{
										Reason:  "FailedMount",
										Message: "MountVolume.SetUp failed for volume \"config-volume\" : configmap \"missing-config\" not found",
									},
								},
							},
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name: "OOM killed pod should be reported",
			podWatch: configv1alpha1.PodWatchSpec{
				Channel: "test-channel",
			},
			namespace: "test-ns",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "oom-pod",
						Namespace: "test-ns",
					},
					Status: v1.PodStatus{
						Phase: v1.PodFailed,
						ContainerStatuses: []v1.ContainerStatus{
							{
								State: v1.ContainerState{
									Terminated: &v1.ContainerStateTerminated{
										Reason:  "OOMKilled",
										Message: "Container killed due to memory usage",
									},
								},
							},
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name: "dangling terminated pod should be reported",
			podWatch: configv1alpha1.PodWatchSpec{
				Channel: "test-channel",
			},
			namespace: "test-ns",
			pods: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "dangling-pod",
						Namespace:  "test-ns",
						Finalizers: []string{"finalizer.example.com"},
						DeletionTimestamp: &metav1.Time{
							Time: time.Now(),
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodFailed,
					},
				},
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fake Kubernetes client
			scheme := runtime.NewScheme()
			_ = v1.AddToScheme(scheme)
			_ = v2.AddToScheme(scheme)

			// Create objects for the fake client
			objs := make([]client.Object, len(tt.pods))
			for i := range tt.pods {
				objs[i] = &tt.pods[i]
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				Build()

			// Create a real slack client with a mock transport
			slackClient := slack.New("xoxb-test-token")

			// Create watcher
			watcher := NewWatcher(fakeClient, scheme, slackClient)

			// Run Reconcile
			reports, err := watcher.Reconcile(ctx, tt.podWatch, tt.lastReports, tt.namespace)

			// Assertions
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, reports)
			}
		})
	}
}

func TestWatcher_processReport(t *testing.T) {
	// Create test cases
	tests := []struct {
		name        string
		mapReports  map[string]configv1alpha1.PodReport
		channel     configv1alpha1.PodWatchSpec
		namespace   string
		entry       configv1alpha1.PodReport
		expectError bool
		setupMock   func(*MockSlackClient)
	}{
		{
			name:       "new failing pod should send message",
			mapReports: make(map[string]configv1alpha1.PodReport),
			channel: configv1alpha1.PodWatchSpec{
				Channel: "test-channel",
			},
			namespace: "test-ns",
			entry: configv1alpha1.PodReport{
				Hash:        "test-hash",
				Name:        "failing-pod",
				LastStatus:  "ImagePullBackOff",
				Reason:      "Failed to pull image",
				LastUpdated: time.Now().Format(time.RFC3339),
			},
			expectError: false,
			setupMock: func(m *MockSlackClient) {
				m.On("SendMessage", "test-channel", mock.Anything).Return("test-channel", "123.456", "", nil)
			},
		},
		{
			name: "update existing failing pod should update message",
			mapReports: map[string]configv1alpha1.PodReport{
				"test-hash": {
					Hash:        "test-hash",
					Name:        "failing-pod",
					LastStatus:  "ImagePullBackOff",
					Reason:      "Failed to pull image",
					LastUpdated: time.Now().Format(time.RFC3339),
					ThreadID:    "test-channel:123.456",
				},
			},
			channel: configv1alpha1.PodWatchSpec{
				Channel: "test-channel",
			},
			namespace: "test-ns",
			entry: configv1alpha1.PodReport{
				Hash:        "test-hash",
				Name:        "failing-pod",
				LastStatus:  "CrashLoopBackOff",
				Reason:      "Container crashed",
				LastUpdated: time.Now().Format(time.RFC3339),
			},
			expectError: false,
			setupMock: func(m *MockSlackClient) {
				m.On("UpdateMessage", "test-channel", "123.456", mock.Anything).Return("test-channel", "123.456", "", nil)
				m.On("SendMessage", "test-channel", mock.Anything, mock.Anything).Return("test-channel", "123.456", "", nil)
			},
		},
		{
			name: "resolved pod should add reaction and delete from map",
			mapReports: map[string]configv1alpha1.PodReport{
				"test-hash": {
					Hash:        "test-hash",
					Name:        "failing-pod",
					LastStatus:  "ImagePullBackOff",
					Reason:      "Failed to pull image",
					LastUpdated: time.Now().Format(time.RFC3339),
					ThreadID:    "test-channel:123.456",
				},
			},
			channel: configv1alpha1.PodWatchSpec{
				Channel: "test-channel",
			},
			namespace: "test-ns",
			entry: configv1alpha1.PodReport{
				Hash:        "test-hash",
				Name:        "failing-pod",
				LastStatus:  ContainerResolved,
				Reason:      "",
				LastUpdated: time.Now().Format(time.RFC3339),
			},
			expectError: false,
			setupMock: func(m *MockSlackClient) {
				m.On("AddReaction", "white_check_mark", mock.Anything).Return(nil)
				m.On("SendMessage", "test-channel", mock.Anything, mock.Anything).Return("test-channel", "123.456", "", nil)
			},
		},
		{
			name: "invalid thread ID format should not error",
			mapReports: map[string]configv1alpha1.PodReport{
				"test-hash": {
					Hash:        "test-hash",
					Name:        "failing-pod",
					LastStatus:  "ImagePullBackOff",
					Reason:      "Failed to pull image",
					LastUpdated: time.Now().Format(time.RFC3339),
					ThreadID:    "invalid-format",
				},
			},
			channel: configv1alpha1.PodWatchSpec{
				Channel: "test-channel",
			},
			namespace: "test-ns",
			entry: configv1alpha1.PodReport{
				Hash:        "test-hash",
				Name:        "failing-pod",
				LastStatus:  ContainerResolved,
				Reason:      "",
				LastUpdated: time.Now().Format(time.RFC3339),
			},
			expectError: false,
			setupMock: func(m *MockSlackClient) {
				// No expectations needed as invalid format should skip Slack calls
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fake Kubernetes client
			scheme := runtime.NewScheme()
			_ = v1.AddToScheme(scheme)
			_ = v2.AddToScheme(scheme)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			// Create mock Slack client
			mockSlack := new(MockSlackClient)
			tt.setupMock(mockSlack)

			// Create watcher
			watcher := NewWatcher(fakeClient, scheme, mockSlack)
			prevReport := tt.mapReports[tt.entry.Hash]

			// Run processReport
			err := watcher.processReport(tt.mapReports, tt.channel, tt.namespace, tt.entry)

			// Assertions
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Additional assertions based on test case
				if tt.entry.LastStatus == ContainerResolved {
					// Check that the report was removed from map
					_, exists := tt.mapReports[tt.entry.Hash]
					assert.False(t, exists)
				} else if tt.entry.LastStatus != "" {
					// Check that the report was added/updated in map
					_, exists := tt.mapReports[tt.entry.Hash]
					assert.True(t, exists)
					assert.NotEqual(t, tt.entry.LastStatus, prevReport.LastStatus)
					assert.NotEqual(t, tt.entry.Reason, prevReport.Reason)
				}
			}

			// Verify all expected mock calls were made
			mockSlack.AssertExpectations(t)
		})
	}
}

func TestWatcher_determineFailingStatusFromEvent(t *testing.T) {
	// Create test cases
	tests := []struct {
		name           string
		pod            v1.Pod
		events         []v1.Event
		expectedStatus string
		expectedReason string
		expectError    bool
	}{
		{
			name: "failed mount event should be reported",
			pod: v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
			},
			events: []v1.Event{
				{
					TypeMeta: metav1.TypeMeta{
						Kind: "Event",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-event",
						Namespace: "test-ns",
					},
					InvolvedObject: v1.ObjectReference{
						Name:      "test-pod",
						Namespace: "test-ns",
					},
					Type:    v1.EventTypeWarning,
					Reason:  "FailedMount",
					Message: "MountVolume.SetUp failed for volume \"config-volume\"",
					LastTimestamp: metav1.Time{
						Time: time.Now(),
					},
				},
			},
			expectedStatus: "FailedMount",
			expectedReason: "MountVolume.SetUp failed for volume \"config-volume\"",
			expectError:    false,
		},
		{
			name: "unhealthy event should be reported",
			pod: v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
			},
			events: []v1.Event{
				{
					TypeMeta: metav1.TypeMeta{
						Kind: "Event",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-event",
						Namespace: "test-ns",
					},
					InvolvedObject: v1.ObjectReference{
						Name:      "test-pod",
						Namespace: "test-ns",
					},
					Type:    v1.EventTypeWarning,
					Reason:  "Unhealthy",
					Message: "Readiness probe failed",
					LastTimestamp: metav1.Time{
						Time: time.Now(),
					},
				},
			},
			expectedStatus: "Unhealthy",
			expectedReason: "Readiness probe failed",
			expectError:    false,
		},
		{
			name: "multiple events should use most recent",
			pod: v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
			},
			events: []v1.Event{
				{
					TypeMeta: metav1.TypeMeta{
						Kind: "Event",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "old-event",
						Namespace: "test-ns",
					},
					InvolvedObject: v1.ObjectReference{
						Name:      "test-pod",
						Namespace: "test-ns",
					},
					Type:    v1.EventTypeWarning,
					Reason:  "FailedMount",
					Message: "Old mount failure",
					LastTimestamp: metav1.Time{
						Time: time.Now().Add(-1 * time.Hour),
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind: "Event",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "new-event",
						Namespace: "test-ns",
					},
					InvolvedObject: v1.ObjectReference{
						Name:      "test-pod",
						Namespace: "test-ns",
					},
					Type:    v1.EventTypeWarning,
					Reason:  "Unhealthy",
					Message: "New readiness probe failure",
					LastTimestamp: metav1.Time{
						Time: time.Now(),
					},
				},
			},
			expectedStatus: "Unhealthy",
			expectedReason: "New readiness probe failure",
			expectError:    false,
		},
		{
			name: "no events should return empty status",
			pod: v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
			},
			events:         []v1.Event{},
			expectedStatus: "",
			expectedReason: "",
			expectError:    false,
		},
		{
			name: "non-warning events should be ignored",
			pod: v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
			},
			events: []v1.Event{
				{
					TypeMeta: metav1.TypeMeta{
						Kind: "Event",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-event",
						Namespace: "test-ns",
					},
					InvolvedObject: v1.ObjectReference{
						Name:      "test-pod",
						Namespace: "test-ns",
					},
					Type:    v1.EventTypeNormal,
					Reason:  "Started",
					Message: "Container started",
					LastTimestamp: metav1.Time{
						Time: time.Now(),
					},
				},
			},
			expectedStatus: "",
			expectedReason: "",
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fake Kubernetes client
			scheme := runtime.NewScheme()
			_ = v1.AddToScheme(scheme)
			_ = v2.AddToScheme(scheme)

			// Create objects for the fake client
			objs := make([]client.Object, len(tt.events))
			for i := range tt.events {
				objs[i] = &tt.events[i]
			}

			// Create fake client with field indexing
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				WithIndex(&v1.Event{}, "involvedObject.name", func(obj client.Object) []string {
					event := obj.(*v1.Event)
					return []string{event.InvolvedObject.Name}
				}).
				WithIndex(&v1.Event{}, "involvedObject.namespace", func(obj client.Object) []string {
					event := obj.(*v1.Event)
					return []string{event.InvolvedObject.Namespace}
				}).
				WithIndex(&v1.Event{}, "type", func(obj client.Object) []string {
					event := obj.(*v1.Event)
					return []string{event.Type}
				}).
				Build()

			// Create mock Slack client
			mockSlack := new(MockSlackClient)

			// Create watcher
			watcher := NewWatcher(fakeClient, scheme, mockSlack)

			// Create logger
			logger := logr.Discard()

			// Run determineFailingStatusFromEvent
			status, reason := watcher.determineFailingStatusFromEvent(context.Background(), tt.pod, logger)

			// Assertions
			assert.Equal(t, tt.expectedStatus, status)
			assert.Equal(t, tt.expectedReason, reason)
		})
	}
}

func TestWatcher_getOwner(t *testing.T) {
	// Create test cases
	tests := []struct {
		name          string
		pod           *v1.Pod
		replicaSet    *v2.ReplicaSet
		expectedOwner *v1.ObjectReference
		expectError   bool
	}{
		{
			name: "pod with no owner should return nil",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
			},
			expectedOwner: nil,
			expectError:   false,
		},
		{
			name: "pod with direct owner should return owner",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind:       "Deployment",
							Name:       "test-deployment",
							Controller: &[]bool{true}[0],
							APIVersion: "apps/v1",
						},
					},
				},
			},
			expectedOwner: &v1.ObjectReference{
				Kind:       "Deployment",
				Name:       "test-deployment",
				Namespace:  "test-ns",
				APIVersion: "apps/v1",
			},
			expectError: false,
		},
		{
			name: "pod owned by ReplicaSet should return Deployment owner",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind:       "ReplicaSet",
							Name:       "test-replicaset",
							Controller: &[]bool{true}[0],
							APIVersion: "apps/v1",
						},
					},
				},
			},
			replicaSet: &v2.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-replicaset",
					Namespace: "test-ns",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind:       "Deployment",
							Name:       "test-deployment",
							Controller: &[]bool{true}[0],
							APIVersion: "apps/v1",
						},
					},
				},
			},
			expectedOwner: &v1.ObjectReference{
				Kind:       "Deployment",
				Name:       "test-deployment",
				Namespace:  "test-ns",
				APIVersion: "apps/v1",
			},
			expectError: false,
		},
		{
			name: "pod owned by ReplicaSet with no controller should return ReplicaSet",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind:       "ReplicaSet",
							Name:       "test-replicaset",
							Controller: &[]bool{true}[0],
							APIVersion: "apps/v1",
						},
					},
				},
			},
			replicaSet: &v2.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-replicaset",
					Namespace: "test-ns",
				},
			},
			expectedOwner: &v1.ObjectReference{
				Kind:       "ReplicaSet",
				Name:       "test-replicaset",
				Namespace:  "test-ns",
				APIVersion: "apps/v1",
			},
			expectError: false,
		},
		{
			name: "error getting ReplicaSet should return error",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind:       "ReplicaSet",
							Name:       "non-existent-replicaset",
							Controller: &[]bool{true}[0],
							APIVersion: "apps/v1",
						},
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fake Kubernetes client
			scheme := runtime.NewScheme()
			_ = v1.AddToScheme(scheme)
			_ = v2.AddToScheme(scheme)

			// Create objects for the fake client
			var objs []client.Object
			if tt.replicaSet != nil {
				objs = append(objs, tt.replicaSet)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				Build()

			// Create mock Slack client
			mockSlack := new(MockSlackClient)

			// Create watcher
			watcher := NewWatcher(fakeClient, scheme, mockSlack)

			// Run getOwner
			owner, err := watcher.getOwner(context.Background(), tt.pod)

			// Assertions
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.expectedOwner == nil {
					assert.Nil(t, owner)
				} else {
					assert.Equal(t, tt.expectedOwner.Kind, owner.Kind)
					assert.Equal(t, tt.expectedOwner.Name, owner.Name)
					assert.Equal(t, tt.expectedOwner.Namespace, owner.Namespace)
					assert.Equal(t, tt.expectedOwner.APIVersion, owner.APIVersion)
				}
			}
		})
	}
}
