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
	"bytes"
	"context"
	"encoding/json"
	deployv1alpha1 "github.com/thomas-elliott/k8s-deploy-monitor/api/v1alpha1"
	"io"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"net/http"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
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
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *MonitorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "Failed to retrieve Pod")
			return ctrl.Result{}, err
		}

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
	log := log.FromContext(ctx)

	log.Info("Processing Pod", "name", pod)

	payload := map[string]interface{}{
		"pod":       pod.Name,
		"namespace": pod.Namespace,
		"image":     pod.Spec.Containers[0].Image,
		"status":    pod.Status,
		"labels":    pod.ObjectMeta.Labels,
	}

	r.sendPayloadToWebhook(ctx, "pod", payload)

	return ctrl.Result{}, nil
}

// Separate handling logic for Deployments into its own method
func (r *MonitorReconciler) handleDeployment(ctx context.Context, deployment *appsv1.Deployment) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Processing Deployment", "name", deployment)

	payload := map[string]interface{}{
		"deployment":      deployment.Name,
		"namespace":       deployment.Namespace,
		"image":           deployment.Spec.Template.Spec.Containers[0].Image,
		"readyReplicas":   deployment.Status.ReadyReplicas,
		"desiredReplicas": deployment.Status.Replicas,
		"status":          deployment.Status,
		"labels":          deployment.ObjectMeta.Labels,
	}

	r.sendPayloadToWebhook(ctx, "deployment", payload)

	return ctrl.Result{}, nil
}

func (r *MonitorReconciler) sendPayloadToWebhook(ctx context.Context, path string, payload interface{}) {
	log := log.FromContext(ctx)

	var monitorInstance deployv1alpha1.Monitor
	if err := r.Get(ctx, types.NamespacedName{Name: "monitor-config", Namespace: "default"}, &monitorInstance); err != nil {
		// handle error
		log.Error(err, "Failed to retrieve Monitor")
	}

	endpoint := monitorInstance.Spec.DeploymentEndpoint
	if path == "pod" {
		endpoint = monitorInstance.Spec.PodEndpoint
	}
	apiKeyHeader := monitorInstance.Spec.APIKeyHeader
	if endpoint == "" {
		log.Info("No webhook set up")
		return
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Error(err, "Failed to marshal payload")
		return
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Error(err, "Failed to create request")
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if monitorInstance.Spec.APIKey != nil {
		req.Header.Set(apiKeyHeader, *monitorInstance.Spec.APIKey)
	}

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Error(err, "Failed to send request")
		return
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error(err, "Failed to close response body")
		}
	}(resp.Body)
}

// SetupWithManager sets up the controller with the Manager.
func (r *MonitorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&deployv1alpha1.Monitor{}).
		Watches(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForObject{}).
		Watches(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
