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
	"regexp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strings"
	"time"
)

// MonitorReconciler reconciles a Monitor object
type MonitorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config *MonitorConfig
}

type MonitorConfig struct {
	NamespaceRegex    *regexp.Regexp
	PodWebhook        string
	DeploymentWebhook string
	ReplicaWebhook    string
	APIKey            string
	APIKeyHeader      string
	IsReady           bool
}

type ImageDetails struct {
	Name     string `json:"name"`
	FullName string `json:"fullName"`
	Tag      string `json:"tag"`
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
	// Early exit if the config doesn't exist
	if r.Config == nil || r.Config.IsReady == false {
		return ctrl.Result{}, nil
	}

	log := log.FromContext(ctx)

	var pod corev1.Pod
	var deployment appsv1.Deployment
	var replicaSet appsv1.ReplicaSet

	matches, err := r.namespaceMatchesFilter(req.Namespace)
	if err != nil {
		log.Error(err, "Namespace regex had error")
		return ctrl.Result{}, err
	}
	if !matches {
		return ctrl.Result{}, nil
	}

	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "Failed to retrieve Pod")
			return ctrl.Result{}, err
		}
	} else {
		return r.handlePod(ctx, &pod)
	}

	if err := r.Get(ctx, req.NamespacedName, &deployment); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "Failed to retrieve Deployment")
			return ctrl.Result{}, err
		}
	} else {
		return r.handleDeployment(ctx, &deployment)
	}

	if err := r.Get(ctx, req.NamespacedName, &replicaSet); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "Failed to retrieve ReplicaSet")
			return ctrl.Result{}, err
		}
	} else {
		return r.handleReplicaSet(ctx, &replicaSet)
	}

	return ctrl.Result{}, nil
}

func (r *MonitorReconciler) refreshConfig(ctx context.Context) error {
	log := log.FromContext(ctx)

	var monitor deployv1alpha1.Monitor

	if err := r.Get(ctx, types.NamespacedName{Name: "monitor-config", Namespace: "default"}, &monitor); err != nil {
		log.Error(err, "Failed to retrieve Monitor")
		return err
	}

	if monitor.Spec.NamespaceRegex != nil {
		regex, err := regexp.Compile(*monitor.Spec.NamespaceRegex)
		if err != nil {
			return err
		}
		r.Config.NamespaceRegex = regex
	} else {
		r.Config.NamespaceRegex = nil
	}

	r.Config.DeploymentWebhook = monitor.Spec.DeploymentEndpoint
	r.Config.PodWebhook = monitor.Spec.PodEndpoint
	r.Config.ReplicaWebhook = monitor.Spec.ReplicaEndpoint

	if monitor.Spec.APIKey != nil {
		r.Config.APIKey = *monitor.Spec.APIKey
	}

	if monitor.Spec.APIKeyHeader != "" {
		r.Config.APIKeyHeader = monitor.Spec.APIKeyHeader
	} else {
		r.Config.APIKeyHeader = "X-Api-Key"
	}

	log.Info("Refreshed config")

	return nil
}

func (r *MonitorReconciler) namespaceMatchesFilter(namespace string) (bool, error) {
	if r.Config.NamespaceRegex == nil {
		return true, nil
	}

	matched := r.Config.NamespaceRegex.MatchString(namespace)

	return matched, nil
}

func (r *MonitorReconciler) handlePod(ctx context.Context, pod *corev1.Pod) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Processing Pod")

	payload := map[string]interface{}{
		"pod":       pod.Name,
		"namespace": pod.Namespace,
		"images":    ParseContainers(pod.Spec.Containers),
		"status":    pod.Status,
		"labels":    pod.ObjectMeta.Labels,
	}

	r.sendPayloadToWebhook(ctx, "pod", payload)

	return ctrl.Result{}, nil
}

func (r *MonitorReconciler) handleDeployment(ctx context.Context, deployment *appsv1.Deployment) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Processing Deployment")

	payload := map[string]interface{}{
		"deployment":      deployment.Name,
		"revision":        deployment.Annotations["deployment.kubernetes.io/revision"],
		"namespace":       deployment.Namespace,
		"images":          ParseContainers(deployment.Spec.Template.Spec.Containers),
		"readyReplicas":   deployment.Status.ReadyReplicas,
		"desiredReplicas": deployment.Status.Replicas,
		"status":          deployment.Status,
		"labels":          deployment.ObjectMeta.Labels,
	}

	r.sendPayloadToWebhook(ctx, "deployment", payload)

	return ctrl.Result{}, nil
}

func (r *MonitorReconciler) handleReplicaSet(ctx context.Context, replica *appsv1.ReplicaSet) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Processing ReplicaSet")

	payload := map[string]interface{}{
		"replicaSet":  replica.Name,
		"namespace":   replica.Namespace,
		"revision":    replica.Annotations["deployment.kubernetes.io/revision"],
		"labels":      replica.ObjectMeta.Labels,
		"annotations": replica.Annotations,
		"status":      replica.Status,
		"images":      ParseContainers(replica.Spec.Template.Spec.Containers),
		"replicas":    replica.Spec.Replicas,
	}

	r.sendPayloadToWebhook(ctx, "replica", payload)

	return ctrl.Result{}, nil
}

func (r *MonitorReconciler) sendPayloadToWebhook(ctx context.Context, path string, payload interface{}) {
	log := log.FromContext(ctx)

	endpoint := r.Config.DeploymentWebhook
	if path == "pod" {
		endpoint = r.Config.PodWebhook
	} else if path == "replica" {
		endpoint = r.Config.ReplicaWebhook
	}

	if endpoint == "" {
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
	if r.Config.APIKey != "" {
		req.Header.Set(r.Config.APIKeyHeader, r.Config.APIKey)
	}

	httpClient := &http.Client{
		Timeout: time.Second * 3,
	}
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

func ParseContainers(containers []corev1.Container) []ImageDetails {
	var images []ImageDetails

	for _, container := range containers {
		images = append(images, ParseContainerImage(container.Image))
	}

	return images
}

func ParseContainerImage(image string) ImageDetails {
	details := ImageDetails{
		FullName: image,
	}

	parts := strings.Split(image, ":")
	if len(parts) > 1 {
		details.Tag = parts[len(parts)-1]
	}

	imageParts := strings.Split(parts[0], "/")
	if len(imageParts) > 1 {
		details.Name = imageParts[len(imageParts)-1]
	}

	return details
}

// SetupWithManager sets up the controller with the Manager.
func (r *MonitorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	monitorEventHandler := handler.EnqueueRequestsFromMapFunc(
		func(a client.Object) []reconcile.Request {
			if r.Config == nil {
				r.Config = &MonitorConfig{}
			}

			// Update the configuration here
			err := r.refreshConfig(context.Background())
			if err != nil {
				log.Log.Error(err, "Failed to refresh config")
				r.Config.IsReady = false
			} else {
				r.Config.IsReady = true
			}

			return []reconcile.Request{
				{NamespacedName: types.NamespacedName{
					Name:      a.GetName(),
					Namespace: a.GetNamespace(),
				}},
			}
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&deployv1alpha1.Monitor{}).
		Watches(&source.Kind{Type: &deployv1alpha1.Monitor{}}, monitorEventHandler).
		Watches(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForObject{}).
		Watches(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForObject{}).
		Watches(&source.Kind{Type: &appsv1.ReplicaSet{}}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
