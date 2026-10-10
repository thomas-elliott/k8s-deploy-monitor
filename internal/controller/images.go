package controller

import (
	"path"

	"github.com/distribution/reference"
	corev1 "k8s.io/api/core/v1"
)

type ImageDetails struct {
	Name     string `json:"name"`
	FullName string `json:"fullName"`
	Tag      string `json:"tag"`
	Digest   string `json:"digest,omitempty"`
}

func ParseContainers(containers []corev1.Container) []ImageDetails {
	images := make([]ImageDetails, 0, len(containers))
	for _, container := range containers {
		images = append(images, ParseContainerImage(container.Image))
	}
	return images
}

func ParseContainerImage(image string) ImageDetails {
	details := ImageDetails{FullName: image}

	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return details
	}

	details.Name = path.Base(reference.Path(named))
	if tagged, ok := named.(reference.Tagged); ok {
		details.Tag = tagged.Tag()
	}
	if digested, ok := named.(reference.Digested); ok {
		details.Digest = digested.Digest().String()
	}
	return details
}
