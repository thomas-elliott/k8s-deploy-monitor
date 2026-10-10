package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	deployv1alpha1 "github.com/thomas-elliott/k8s-deploy-monitor/api/v1alpha1"
)

type webhookRequest struct {
	Path   string
	Header http.Header
	Body   map[string]any
}

type webhookRecorder struct {
	*httptest.Server
	mu       sync.Mutex
	requests []webhookRequest
	failing  atomic.Bool
}

func newWebhookRecorder() *webhookRecorder {
	rec := &webhookRecorder{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec.mu.Lock()
		rec.requests = append(rec.requests, webhookRequest{Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
		rec.mu.Unlock()
		if r.URL.Path == "/flaky" && rec.failing.Load() {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	return rec
}

func (rec *webhookRecorder) received(path, nameKey, name, namespace string) []webhookRequest {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var matched []webhookRequest
	for _, req := range rec.requests {
		if req.Path == path && req.Body[nameKey] == name && req.Body["namespace"] == namespace {
			matched = append(matched, req)
		}
	}
	return matched
}

func createMonitor(name string, spec deployv1alpha1.MonitorSpec) {
	monitor := &deployv1alpha1.Monitor{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: spec}
	Expect(k8sClient.Create(ctx, monitor)).To(Succeed())
	DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, monitor))).To(Succeed()) })
}

func monitorCondition(name, conditionType string) func() *metav1.Condition {
	return func() *metav1.Condition {
		var monitor deployv1alpha1.Monitor
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, &monitor); err != nil {
			return nil
		}
		return meta.FindStatusCondition(monitor.Status.Conditions, conditionType)
	}
}

func haveStatus(status metav1.ConditionStatus, reason string) OmegaMatcher {
	return And(Not(BeNil()), HaveField("Status", status), HaveField("Reason", reason))
}

func createNamespace(name string, labels map[string]string) {
	Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}})).To(Succeed())
}

func newDeployment(namespace, name, image string) *appsv1.Deployment {
	labels := map[string]string{"app": name}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: image}}},
			},
		},
	}
}

var _ = Describe("Monitor", func() {
	BeforeEach(func() {
		SetDefaultEventuallyTimeout(10 * time.Second)
	})

	It("reports an invalid namespace regex as not ready", func() {
		createMonitor("invalid-regex", deployv1alpha1.MonitorSpec{
			NamespaceRegex: "(",
			Endpoints:      deployv1alpha1.Endpoints{Pod: "http://127.0.0.1:1/pod"},
		})

		Eventually(monitorCondition("invalid-regex", deployv1alpha1.ConditionReady)).
			Should(haveStatus(metav1.ConditionFalse, "InvalidNamespaceFilter"))
	})

	It("rejects a Monitor with no endpoints", func() {
		monitor := &deployv1alpha1.Monitor{ObjectMeta: metav1.ObjectMeta{Name: "no-endpoints"}}
		Expect(k8sClient.Create(ctx, monitor)).To(MatchError(ContainSubstring("at least one endpoint is required")))
	})

	It("becomes ready once its API key Secret is created", func() {
		createMonitor("needs-secret", deployv1alpha1.MonitorSpec{
			NamespaceRegex: "^nothing-matches$",
			Endpoints:      deployv1alpha1.Endpoints{Pod: "http://127.0.0.1:1/pod"},
			Auth:           &deployv1alpha1.Auth{SecretRef: deployv1alpha1.SecretKeyRef{Name: "late-secret", Key: "apiKey"}},
		})
		Eventually(monitorCondition("needs-secret", deployv1alpha1.ConditionReady)).
			Should(haveStatus(metav1.ConditionFalse, "SecretNotFound"))

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: secretNamespace, Name: "late-secret"},
			StringData: map[string]string{"apiKey": "k"},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, secret)

		Eventually(monitorCondition("needs-secret", deployv1alpha1.ConditionReady)).
			Should(haveStatus(metav1.ConditionTrue, "Valid"))
	})
})

var _ = Describe("Webhook delivery", Ordered, func() {
	var rec *webhookRecorder

	BeforeAll(func() {
		SetDefaultEventuallyTimeout(10 * time.Second)
		rec = newWebhookRecorder()
		DeferCleanup(rec.Close)

		createNamespace("prod-a", map[string]string{"env": "prod"})
		createNamespace("dev-a", map[string]string{"env": "dev"})

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: secretNamespace, Name: "hook-key"},
			StringData: map[string]string{"apiKey": "s3cret"},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, secret)

		createMonitor("prod", deployv1alpha1.MonitorSpec{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
			Endpoints: deployv1alpha1.Endpoints{
				Deployment: rec.URL + "/deployment",
				ReplicaSet: rec.URL + "/replicaset",
				Pod:        rec.URL + "/pod",
			},
			Auth: &deployv1alpha1.Auth{
				Header:    "Api-Key",
				SecretRef: deployv1alpha1.SecretKeyRef{Name: "hook-key", Key: "apiKey"},
			},
		})
		Eventually(monitorCondition("prod", deployv1alpha1.ConditionReady)).
			Should(haveStatus(metav1.ConditionTrue, "Valid"))
	})

	It("sends Deployments in selected namespaces with the API key, and nothing from other namespaces", func() {
		Expect(k8sClient.Create(ctx, newDeployment("dev-a", "web", "nginx:1.25"))).To(Succeed())
		Expect(k8sClient.Create(ctx, newDeployment("prod-a", "web", "nginx:1.25"))).To(Succeed())

		Eventually(func() []webhookRequest { return rec.received("/deployment", "deployment", "web", "prod-a") }).
			ShouldNot(BeEmpty())
		req := rec.received("/deployment", "deployment", "web", "prod-a")[0]
		Expect(req.Header.Get("Api-Key")).To(Equal("s3cret"))
		Expect(req.Body["images"]).To(ConsistOf(HaveKeyWithValue("name", "nginx")))

		Consistently(func() []webhookRequest { return rec.received("/deployment", "deployment", "web", "dev-a") }).
			Should(BeEmpty())
	})

	It("sends ReplicaSets with their revision and images", func() {
		labels := map[string]string{"app": "web"}
		replicaSet := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   "prod-a",
				Name:        "web-5d8f7c",
				Annotations: map[string]string{"deployment.kubernetes.io/revision": "3"},
			},
			Spec: appsv1.ReplicaSetSpec{
				Selector: &metav1.LabelSelector{MatchLabels: labels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx:1.25"}}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, replicaSet)).To(Succeed())

		Eventually(func() []webhookRequest { return rec.received("/replicaset", "replicaSet", "web-5d8f7c", "prod-a") }).
			ShouldNot(BeEmpty())
		req := rec.received("/replicaset", "replicaSet", "web-5d8f7c", "prod-a")[0]
		Expect(req.Header.Get("Api-Key")).To(Equal("s3cret"))
		Expect(req.Body).To(HaveKeyWithValue("revision", "3"))
		Expect(req.Body["images"]).To(ConsistOf(HaveKeyWithValue("name", "nginx")))
	})

	It("does not resend an unchanged Deployment", func() {
		var deployment appsv1.Deployment
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "prod-a", Name: "web"}, &deployment)).To(Succeed())
		before := len(rec.received("/deployment", "deployment", "web", "prod-a"))

		deployment.Annotations = map[string]string{"unrelated": "change"}
		Expect(k8sClient.Update(ctx, &deployment)).To(Succeed())

		Consistently(func() []webhookRequest { return rec.received("/deployment", "deployment", "web", "prod-a") }).
			Should(HaveLen(before))
	})

	It("sends a deletion", func() {
		Expect(k8sClient.Delete(ctx, newDeployment("prod-a", "web", ""))).To(Succeed())

		Eventually(func() []webhookRequest { return rec.received("/deployment", "deployment", "web", "prod-a") }).
			Should(ContainElement(HaveField("Body", HaveKeyWithValue("deleted", true))))
	})

	It("retries only the Monitor whose webhook failed, reports it on that Monitor, and skips namespaces outside its regex", func() {
		rec.failing.Store(true)
		createMonitor("flaky", deployv1alpha1.MonitorSpec{
			NamespaceRegex: "^prod-",
			Endpoints:      deployv1alpha1.Endpoints{Pod: rec.URL + "/flaky"},
		})
		Eventually(monitorCondition("flaky", deployv1alpha1.ConditionReady)).
			Should(haveStatus(metav1.ConditionTrue, "Valid"))

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "prod-a", Name: "worker"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "busybox"}}},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		devPod := pod.DeepCopy()
		devPod.Namespace = "dev-a"
		devPod.ResourceVersion = ""
		Expect(k8sClient.Create(ctx, devPod)).To(Succeed())

		Eventually(monitorCondition("flaky", deployv1alpha1.ConditionDelivering)).
			Should(haveStatus(metav1.ConditionFalse, "DeliveryFailed"))

		rec.failing.Store(false)
		Eventually(monitorCondition("flaky", deployv1alpha1.ConditionDelivering), 30*time.Second).
			Should(haveStatus(metav1.ConditionTrue, "Delivered"))

		Expect(len(rec.received("/flaky", "pod", "worker", "prod-a"))).To(BeNumerically(">=", 2))
		Expect(rec.received("/pod", "pod", "worker", "prod-a")).To(HaveLen(1))
		Expect(rec.received("/flaky", "pod", "worker", "dev-a")).To(BeEmpty())
	})
})
