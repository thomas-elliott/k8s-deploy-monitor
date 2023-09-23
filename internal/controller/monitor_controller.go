/*
Copyright 2023.

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

package controller

import (
	"context"
	deployv1alpha1 "github.com/thomas-elliott/k8s-deploy-monitor/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// MonitorReconciler reconciles a Monitor object
type MonitorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=deploy.deploy-monitor.local,resources=monitors,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=deploy.deploy-monitor.local,resources=monitors/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=deploy.deploy-monitor.local,resources=monitors/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Monitor object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *MonitorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var monitorInstance deployv1alpha1.Monitor
	if err := r.Get(ctx, types.NamespacedName{Name: "monitor-config", Namespace: "default"}, &monitorInstance); err != nil {
		// handle error
		log.Error(err, "Failed to retrieve Monitor")
	}

	endpoint := monitorInstance.Spec.WebhookEndpoint
	apiKey := monitorInstance.Spec.APIKey
	log.Info("Processing Monitor", "name", monitorInstance.Name, "endpoint", endpoint, "apiKey", apiKey)

	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "Failed to retrieve Pod")
			return ctrl.Result{}, err
		}

		// If the error indicates the resource isn't a Pod, try fetching a Deployment
		var deployment appsv1.Deployment
		if err := r.Get(ctx, req.NamespacedName, &deployment); err != nil {
			if errors.IsNotFound(err) {
				log.Info("Neither Pod nor Deployment found, might have been deleted")
				return ctrl.Result{}, nil
			}
			log.Error(err, "Failed to retrieve Deployment")
			return ctrl.Result{}, err
		}

		return r.handleDeployment(ctx, &deployment)
	}

	return r.handlePod(ctx, &pod)
}

func (r *MonitorReconciler) handlePod(ctx context.Context, pod *corev1.Pod) (ctrl.Result, error) {
	// TODO: So far the pods haven't been needed, leaving it behind for now
	log := log.FromContext(ctx)

	status := pod.Status
	log.Info("Processing Pod", "name", pod.Name, "status", status)

	return ctrl.Result{}, nil
}

// Separate handling logic for Deployments into its own method
func (r *MonitorReconciler) handleDeployment(ctx context.Context, deployment *appsv1.Deployment) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	readyReplicas := deployment.Status.ReadyReplicas
	desiredReplicas := deployment.Status.Replicas
	image := deployment.Spec.Template.Spec.Containers[0].Image
	log.Info("Processing Deployment", "name", deployment.Name, "readyReplicas", readyReplicas, "desiredReplicas", desiredReplicas, "image", image)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MonitorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		Owns(&corev1.Pod{}). // TODO: This isn't watching the pods
		Complete(r)
}
