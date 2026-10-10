package controller

import (
	"context"
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	deployv1alpha1 "github.com/thomas-elliott/k8s-deploy-monitor/api/v1alpha1"
)

type MonitorReconciler struct {
	client.Client
	SecretNamespace string
}

// +kubebuilder:rbac:groups=deploy.deploy-monitor.local,resources=monitors,verbs=get;list;watch
// +kubebuilder:rbac:groups=deploy.deploy-monitor.local,resources=monitors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",namespace=system,resources=secrets,verbs=get;list;watch

func (r *MonitorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var monitor deployv1alpha1.Monitor
	if err := r.Get(ctx, req.NamespacedName, &monitor); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ready, err := r.validate(ctx, &monitor)
	if err != nil {
		return ctrl.Result{}, err
	}
	ready.Type = deployv1alpha1.ConditionReady
	ready.ObservedGeneration = monitor.Generation

	changed := meta.SetStatusCondition(&monitor.Status.Conditions, ready)
	if !changed && monitor.Status.ObservedGeneration == monitor.Generation {
		return ctrl.Result{}, nil
	}
	monitor.Status.ObservedGeneration = monitor.Generation
	return ctrl.Result{}, r.Status().Update(ctx, &monitor)
}

func (r *MonitorReconciler) validate(ctx context.Context, monitor *deployv1alpha1.Monitor) (metav1.Condition, error) {
	if _, err := newNamespaceMatcher(monitor.Spec); err != nil {
		return notReady("InvalidNamespaceFilter", err.Error()), nil
	}

	if monitor.Spec.Auth != nil {
		_, err := apiKey(ctx, r.Client, r.SecretNamespace, monitor.Spec.Auth.SecretRef)
		if apierrors.IsNotFound(err) {
			return notReady("SecretNotFound", fmt.Sprintf("secret %s/%s not found", r.SecretNamespace, monitor.Spec.Auth.SecretRef.Name)), nil
		}
		if err != nil {
			if _, missing := err.(errSecretKeyMissing); missing {
				return notReady("SecretKeyMissing", err.Error()), nil
			}
			return metav1.Condition{}, err
		}
	}

	return metav1.Condition{Status: metav1.ConditionTrue, Reason: "Valid"}, nil
}

func notReady(reason, message string) metav1.Condition {
	return metav1.Condition{Status: metav1.ConditionFalse, Reason: reason, Message: message}
}

func (r *MonitorReconciler) monitorsForSecret(ctx context.Context, secret client.Object) []reconcile.Request {
	var monitors deployv1alpha1.MonitorList
	if err := r.List(ctx, &monitors); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for _, m := range monitors.Items {
		if m.Spec.Auth != nil && m.Spec.Auth.SecretRef.Name == secret.GetName() {
			requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Name: m.Name}})
		}
	}
	return requests
}

func (r *MonitorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&deployv1alpha1.Monitor{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.monitorsForSecret)).
		Named("monitor").
		Complete(r)
}

type namespaceMatcher struct {
	selector labels.Selector
	regex    *regexp.Regexp
}

func newNamespaceMatcher(spec deployv1alpha1.MonitorSpec) (*namespaceMatcher, error) {
	m := &namespaceMatcher{selector: labels.Everything()}
	if spec.NamespaceSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(spec.NamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("namespaceSelector: %w", err)
		}
		m.selector = selector
	}
	if spec.NamespaceRegex != "" {
		regex, err := regexp.Compile(spec.NamespaceRegex)
		if err != nil {
			return nil, fmt.Errorf("namespaceRegex: %w", err)
		}
		m.regex = regex
	}
	return m, nil
}

func (m *namespaceMatcher) matches(name string, namespaceLabels map[string]string) bool {
	if m.regex != nil && !m.regex.MatchString(name) {
		return false
	}
	return m.selector.Matches(labels.Set(namespaceLabels))
}

type errSecretKeyMissing struct{ namespace, name, key string }

func (e errSecretKeyMissing) Error() string {
	return fmt.Sprintf("secret %s/%s has no key %q", e.namespace, e.name, e.key)
}

func apiKey(ctx context.Context, c client.Reader, namespace string, ref deployv1alpha1.SecretKeyRef) (string, error) {
	var secret corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, &secret); err != nil {
		return "", err
	}
	value, ok := secret.Data[ref.Key]
	if !ok {
		return "", errSecretKeyMissing{namespace, ref.Name, ref.Key}
	}
	return string(value), nil
}
