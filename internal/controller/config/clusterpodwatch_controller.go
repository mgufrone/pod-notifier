/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "github.com/mgufrone/pod-notifier/api/config/v1alpha1"
	"github.com/mgufrone/pod-notifier/internal/service"
)

// ClusterPodWatchReconciler reconciles a ClusterPodWatch object
type ClusterPodWatchReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	svc    *service.Watcher
}

func NewClusterPodWatchReconciler(svc *service.Watcher) *ClusterPodWatchReconciler {
	return &ClusterPodWatchReconciler{
		Client: svc.Client,
		Scheme: svc.Scheme,
		svc:    svc,
	}
}

// +kubebuilder:rbac:groups=config.mgufrone.dev,resources=clusterpodwatches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.mgufrone.dev,resources=clusterpodwatches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=config.mgufrone.dev,resources=clusterpodwatches/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the ClusterPodWatch object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.2/pkg/reconcile
func (r *ClusterPodWatchReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	var (
		clusterPodWatch configv1alpha1.ClusterPodWatch
		logger          = ctrl.LoggerFrom(ctx)
	)
	if err = r.Get(ctx, req.NamespacedName, &clusterPodWatch); err != nil {
		logger.Error(err, "unable to fetch ClusterPodWatch")
	}
	reports, err := r.svc.Reconcile(ctx, clusterPodWatch.Spec.PodWatchSpec, clusterPodWatch.Status.Reports, "")
	res = ctrl.Result{
		RequeueAfter: time.Second * time.Duration(clusterPodWatch.Spec.PodWatchSpec.Interval),
	}
	if err != nil {
		logger.Error(err, "unable to reconcile PodWatch")
		return
	}
	clusterPodWatch.Status = configv1alpha1.ClusterPodWatchStatus{
		Reports: reports,
	}
	err = r.Status().Update(ctx, &clusterPodWatch)
	return
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterPodWatchReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1alpha1.ClusterPodWatch{}).
		Named("config-clusterpodwatch").
		Complete(r)
}
