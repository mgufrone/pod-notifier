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

	configv1alpha1 "github.com/mgufrone/pod-notifier/api/config/v1alpha1"
	"github.com/mgufrone/pod-notifier/internal/service"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodWatchReconciler reconciles a PodWatch object
type PodWatchReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	svc    *service.Watcher
}

func NewPodWatchReconciler(svc *service.Watcher) *PodWatchReconciler {
	return &PodWatchReconciler{
		Client: svc.Client,
		Scheme: svc.Scheme,
		svc:    svc,
	}
}

// +kubebuilder:rbac:groups=config.mgufrone.dev,resources=podwatches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.mgufrone.dev,resources=podwatches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=config.mgufrone.dev,resources=podwatches/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the PodWatch object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.2/pkg/reconcile
func (r *PodWatchReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	var (
		podWatch configv1alpha1.PodWatch
		logger   = ctrl.LoggerFrom(ctx)
	)
	if err = r.Get(ctx, req.NamespacedName, &podWatch); err != nil {
		logger.Error(err, "unable to fetch PodWatch")
	}
	reports, err := r.svc.Reconcile(ctx, podWatch.Spec, podWatch.Status.Reports, req.Namespace)
	if podWatch.Spec.Interval == 0 {
		podWatch.Spec.Interval = 60
	}
	res = ctrl.Result{
		RequeueAfter: time.Second * time.Duration(podWatch.Spec.Interval),
	}
	if err != nil {
		logger.Error(err, "unable to reconcile PodWatch")
		return
	}
	podWatch.Status = configv1alpha1.PodWatchStatus{
		Reports: reports,
	}
	err = r.Status().Update(ctx, &podWatch)
	return
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodWatchReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1alpha1.PodWatch{}).
		Named("config-podwatch").
		Complete(r)
}
