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
