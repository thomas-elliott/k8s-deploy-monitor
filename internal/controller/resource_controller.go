package controller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	deployv1alpha1 "github.com/thomas-elliott/k8s-deploy-monitor/api/v1alpha1"
)

const revisionAnnotation = "deployment.kubernetes.io/revision"

type resourceKind struct {
	name      string
	newObject func() client.Object
	endpoint  func(deployv1alpha1.Endpoints) string
	payload   func(client.Object) any
}

var resourceKinds = []resourceKind{
	{
		name:      "deployment",
		newObject: func() client.Object { return &appsv1.Deployment{} },
		endpoint:  func(e deployv1alpha1.Endpoints) string { return e.Deployment },
		payload: func(obj client.Object) any {
			d := obj.(*appsv1.Deployment)
			return deploymentPayload{
				Deployment:      d.Name,
				Revision:        d.Annotations[revisionAnnotation],
				Namespace:       d.Namespace,
				Images:          ParseContainers(d.Spec.Template.Spec.Containers),
				ReadyReplicas:   d.Status.ReadyReplicas,
				DesiredReplicas: d.Status.Replicas,
				Status:          d.Status,
				Labels:          d.Labels,
			}
		},
	},
	{
		name:      "replicaSet",
		newObject: func() client.Object { return &appsv1.ReplicaSet{} },
		endpoint:  func(e deployv1alpha1.Endpoints) string { return e.ReplicaSet },
		payload: func(obj client.Object) any {
			rs := obj.(*appsv1.ReplicaSet)
			return replicaSetPayload{
				ReplicaSet:  rs.Name,
				Namespace:   rs.Namespace,
				Revision:    rs.Annotations[revisionAnnotation],
				Labels:      rs.Labels,
				Annotations: rs.Annotations,
				Status:      rs.Status,
				Images:      ParseContainers(rs.Spec.Template.Spec.Containers),
				Replicas:    rs.Spec.Replicas,
			}
		},
	},
	{
		name:      "pod",
		newObject: func() client.Object { return &corev1.Pod{} },
		endpoint:  func(e deployv1alpha1.Endpoints) string { return e.Pod },
		payload: func(obj client.Object) any {
			p := obj.(*corev1.Pod)
			return podPayload{
				Pod:       p.Name,
				Namespace: p.Namespace,
				Images:    ParseContainers(p.Spec.Containers),
				Status:    p.Status,
				Labels:    p.Labels,
			}
		},
	},
}

// +kubebuilder:rbac:groups=apps,resources=deployments;replicasets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods;namespaces,verbs=get;list;watch

type resourceReconciler struct {
	client.Client
	kind            resourceKind
	secretNamespace string
	webhooks        *WebhookClient

	mu   sync.Mutex
	sent map[types.NamespacedName]map[types.UID][sha256.Size]byte
}

func SetupResourceReconcilers(mgr ctrl.Manager, secretNamespace string, webhooks *WebhookClient) error {
	for _, kind := range resourceKinds {
		r := &resourceReconciler{
			Client:          mgr.GetClient(),
			kind:            kind,
			secretNamespace: secretNamespace,
			webhooks:        webhooks,
			sent:            map[types.NamespacedName]map[types.UID][sha256.Size]byte{},
		}
		err := ctrl.NewControllerManagedBy(mgr).
			For(kind.newObject()).
			Named(kind.name).
			Complete(r)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *resourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := r.kind.newObject()
	deleted := false
	var payload any
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		deleted = true
		payload = map[string]any{r.kind.name: req.Name, "namespace": req.Namespace, "deleted": true}
	} else {
		payload = r.kind.payload(obj)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return ctrl.Result{}, err
	}
	sum := sha256.Sum256(body)

	monitors, err := r.matchingMonitors(ctx, req.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	r.mu.Lock()
	previous := r.sent[req.NamespacedName]
	r.mu.Unlock()

	delivered := map[types.UID][sha256.Size]byte{}
	var errs []error
	for i := range monitors {
		monitor := &monitors[i]
		url := r.kind.endpoint(monitor.Spec.Endpoints)
		if url == "" {
			continue
		}
		if previous[monitor.UID] == sum {
			delivered[monitor.UID] = sum
			continue
		}
		err := r.send(ctx, monitor, url, body)
		r.recordDelivery(ctx, monitor, err)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		delivered[monitor.UID] = sum
	}

	r.mu.Lock()
	if deleted && len(errs) == 0 {
		delete(r.sent, req.NamespacedName)
	} else {
		r.sent[req.NamespacedName] = delivered
	}
	r.mu.Unlock()

	return ctrl.Result{}, errors.Join(errs...)
}

func (r *resourceReconciler) matchingMonitors(ctx context.Context, namespace string) ([]deployv1alpha1.Monitor, error) {
	var monitors deployv1alpha1.MonitorList
	if err := r.List(ctx, &monitors); err != nil {
		return nil, err
	}

	var ns corev1.Namespace
	if err := r.Get(ctx, types.NamespacedName{Name: namespace}, &ns); client.IgnoreNotFound(err) != nil {
		return nil, err
	}

	var matched []deployv1alpha1.Monitor
	for _, monitor := range monitors.Items {
		ready := meta.FindStatusCondition(monitor.Status.Conditions, deployv1alpha1.ConditionReady)
		if ready == nil || ready.Status != metav1.ConditionTrue || ready.ObservedGeneration != monitor.Generation {
			continue
		}
		matcher, err := newNamespaceMatcher(monitor.Spec)
		if err != nil || !matcher.matches(namespace, ns.Labels) {
			continue
		}
		matched = append(matched, monitor)
	}
	return matched, nil
}

func (r *resourceReconciler) send(ctx context.Context, monitor *deployv1alpha1.Monitor, url string, body []byte) error {
	headers := http.Header{}
	if auth := monitor.Spec.Auth; auth != nil {
		key, err := apiKey(ctx, r.Client, r.secretNamespace, auth.SecretRef)
		if err != nil {
			return err
		}
		headers.Set(auth.Header, key)
	}
	return r.webhooks.Post(ctx, url, headers, body)
}

func (r *resourceReconciler) recordDelivery(ctx context.Context, monitor *deployv1alpha1.Monitor, sendErr error) {
	condition := metav1.Condition{
		Type:               deployv1alpha1.ConditionDelivering,
		Status:             metav1.ConditionTrue,
		Reason:             "Delivered",
		ObservedGeneration: monitor.Generation,
	}
	if sendErr != nil {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "DeliveryFailed"
		condition.Message = sendErr.Error()
	}

	current := meta.FindStatusCondition(monitor.Status.Conditions, condition.Type)
	if current != nil && current.Status == condition.Status && current.Message == condition.Message {
		return
	}

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var latest deployv1alpha1.Monitor
		if err := r.Get(ctx, client.ObjectKeyFromObject(monitor), &latest); err != nil {
			return err
		}
		meta.SetStatusCondition(&latest.Status.Conditions, condition)
		return r.Status().Update(ctx, &latest)
	})
	if err != nil {
		logf.FromContext(ctx).Error(err, "Failed to update Delivering condition", "monitor", monitor.Name)
	}
}
