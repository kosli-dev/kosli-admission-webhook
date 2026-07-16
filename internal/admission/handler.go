// Package admission implements the /validate AdmissionReview handler.
package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kosli-dev/kosli-admission-webhook/internal/config"
	"github.com/kosli-dev/kosli-admission-webhook/internal/kosli"
	"github.com/kosli-dev/kosli-admission-webhook/internal/resolver"
)

type Server struct {
	Cfg      *config.Config
	Kosli    *kosli.Client
	Resolver *resolver.Resolver
	Log      *slog.Logger
}

func FingerprintFromImage(image string) (string, bool) {
	if i := strings.Index(image, "@sha256:"); i >= 0 {
		return image[i+len("@sha256:"):], true
	}
	return "", false
}

func (s *Server) Validate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}
	var review admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &review); err != nil || review.Request == nil {
		http.Error(w, "invalid AdmissionReview", http.StatusBadRequest)
		return
	}

	var pod corev1.Pod
	allowed, reason := true, ""
	if err := json.Unmarshal(review.Request.Object.Raw, &pod); err != nil {
		allowed, reason = false, "cannot decode pod: "+err.Error()
	} else {
		allowed, reason = s.checkPod(r.Context(), &pod)
	}

	verdict := "allow"
	if !allowed {
		verdict = "deny"
	}
	s.Log.Info("admission decision",
		"verdict", verdict,
		"namespace", review.Request.Namespace,
		"pod", podName(&pod, review.Request),
		"reason", reason,
	)

	review.Response = &admissionv1.AdmissionResponse{
		UID:     review.Request.UID,
		Allowed: allowed,
	}
	if !allowed {
		review.Response.Result = &metav1.Status{Message: reason, Code: http.StatusForbidden}
	}
	review.Request = nil // response does not need to echo the request
	out, _ := json.Marshal(review)
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

func podName(pod *corev1.Pod, req *admissionv1.AdmissionRequest) string {
	if pod.Name != "" {
		return pod.Name
	}
	if pod.GenerateName != "" {
		return pod.GenerateName + "*"
	}
	return req.Name
}

func (s *Server) checkPod(ctx context.Context, pod *corev1.Pod) (bool, string) {
	containers := make([]corev1.Container, 0,
		len(pod.Spec.InitContainers)+len(pod.Spec.Containers)+len(pod.Spec.EphemeralContainers))
	containers = append(containers, pod.Spec.InitContainers...)
	containers = append(containers, pod.Spec.Containers...)
	for _, ec := range pod.Spec.EphemeralContainers {
		containers = append(containers, corev1.Container(ec.EphemeralContainerCommon))
	}

	for _, c := range containers {
		fp, pinned := FingerprintFromImage(c.Image)
		if !pinned {
			if s.Cfg.RequireDigestPinning {
				return false, fmt.Sprintf(
					"image %q is not pinned by sha256 digest; deploy images as repo@sha256:<digest>", c.Image)
			}
			// Cap each resolution well below the webhook's timeoutSeconds
			// so a slow registry surfaces as a reasoned deny, not a
			// webhook timeout subject to failurePolicy.
			rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			resolved, err := s.Resolver.Resolve(rctx, c.Image)
			cancel()
			if err != nil {
				return false, fmt.Sprintf("cannot resolve sha256 digest for image %q: %v", c.Image, err)
			}
			s.Log.Info("resolved tag to digest", "image", c.Image, "fingerprint", resolved)
			fp = resolved
		}
		res := s.Kosli.Assert(fp)
		if !res.Allowed {
			return false, fmt.Sprintf("image %q rejected by Kosli: %s", c.Image, res.Reason)
		}
	}
	return true, ""
}
