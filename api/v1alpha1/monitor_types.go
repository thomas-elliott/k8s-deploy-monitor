package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	ConditionReady      = "Ready"
	ConditionDelivering = "Delivering"
)

// MonitorSpec defines the desired state of Monitor
type MonitorSpec struct {
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// +optional
	NamespaceRegex string `json:"namespaceRegex,omitempty"`

	// +required
	Endpoints Endpoints `json:"endpoints"`

	// +optional
	Auth *Auth `json:"auth,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="has(self.deployment) || has(self.replicaSet) || has(self.pod)",message="at least one endpoint is required"
type Endpoints struct {
	// +kubebuilder:validation:Format=uri
	// +optional
	Deployment string `json:"deployment,omitempty"`

	// +kubebuilder:validation:Format=uri
	// +optional
	ReplicaSet string `json:"replicaSet,omitempty"`

	// +kubebuilder:validation:Format=uri
	// +optional
	Pod string `json:"pod,omitempty"`
}

type Auth struct {
	// +kubebuilder:default=X-Api-Key
	// +optional
	Header string `json:"header,omitempty"`

	// +required
	SecretRef SecretKeyRef `json:"secretRef"`
}

type SecretKeyRef struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`

	// +kubebuilder:validation:MinLength=1
	// +required
	Key string `json:"key"`
}

// MonitorStatus defines the observed state of Monitor.
type MonitorStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the Monitor resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Delivering",type=string,JSONPath=`.status.conditions[?(@.type=="Delivering")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Monitor is the Schema for the monitors API
type Monitor struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Monitor
	// +required
	Spec MonitorSpec `json:"spec"`

	// status defines the observed state of Monitor
	// +optional
	Status MonitorStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// MonitorList contains a list of Monitor
type MonitorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Monitor `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Monitor{}, &MonitorList{})
		return nil
	})
}
