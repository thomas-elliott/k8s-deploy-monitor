package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

type deploymentPayload struct {
	Deployment      string                  `json:"deployment"`
	Revision        string                  `json:"revision"`
	Namespace       string                  `json:"namespace"`
	Images          []ImageDetails          `json:"images"`
	ReadyReplicas   int32                   `json:"readyReplicas"`
	DesiredReplicas int32                   `json:"desiredReplicas"`
	Status          appsv1.DeploymentStatus `json:"status"`
	Labels          map[string]string       `json:"labels"`
}

type replicaSetPayload struct {
	ReplicaSet  string                  `json:"replicaSet"`
	Namespace   string                  `json:"namespace"`
	Revision    string                  `json:"revision"`
	Labels      map[string]string       `json:"labels"`
	Annotations map[string]string       `json:"annotations"`
	Status      appsv1.ReplicaSetStatus `json:"status"`
	Images      []ImageDetails          `json:"images"`
	Replicas    *int32                  `json:"replicas"`
}

type podPayload struct {
	Pod       string            `json:"pod"`
	Namespace string            `json:"namespace"`
	Images    []ImageDetails    `json:"images"`
	Status    corev1.PodStatus  `json:"status"`
	Labels    map[string]string `json:"labels"`
}
