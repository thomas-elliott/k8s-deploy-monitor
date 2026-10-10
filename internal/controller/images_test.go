package controller

import "testing"

func TestParseContainerImage(t *testing.T) {
	const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	tests := []struct {
		image string
		want  ImageDetails
	}{
		{"nginx:1.25", ImageDetails{Name: "nginx", Tag: "1.25"}},
		{"nginx", ImageDetails{Name: "nginx"}},
		{"registry.example.com/team/app:v2", ImageDetails{Name: "app", Tag: "v2"}},
		{"localhost:5000/app", ImageDetails{Name: "app"}},
		{"localhost:5000/team/app:v2", ImageDetails{Name: "app", Tag: "v2"}},
		{"ghcr.io/org/app@" + digest, ImageDetails{Name: "app", Digest: digest}},
		{"ghcr.io/org/app:1.0@" + digest, ImageDetails{Name: "app", Tag: "1.0", Digest: digest}},
		{"Not A Valid Image", ImageDetails{}},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			tt.want.FullName = tt.image
			if got := ParseContainerImage(tt.image); got != tt.want {
				t.Errorf("ParseContainerImage(%q) = %+v, want %+v", tt.image, got, tt.want)
			}
		})
	}
}
