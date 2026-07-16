package admission

import (
	"strings"
	"testing"
)

func TestFingerprintFromImage(t *testing.T) {
	digest := strings.Repeat("c", 64)
	tests := []struct {
		image  string
		want   string
		pinned bool
	}{
		{image: "busybox@sha256:" + digest, want: digest, pinned: true},
		{image: "ghcr.io/org/app:v1@sha256:" + digest, want: digest, pinned: true},
		{image: "busybox:latest"},
		{image: "ghcr.io/org/app:v1"},
		{image: "busybox"},
	}
	for _, tt := range tests {
		got, pinned := FingerprintFromImage(tt.image)
		if got != tt.want || pinned != tt.pinned {
			t.Errorf("FingerprintFromImage(%q) = (%q, %v), want (%q, %v)",
				tt.image, got, pinned, tt.want, tt.pinned)
		}
	}
}
