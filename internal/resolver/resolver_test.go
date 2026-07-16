package resolver

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	regfake "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// fakeRegistry serves the OCI Distribution API in-process and returns the
// host (127.0.0.1:port, which go-containerregistry treats as plain HTTP)
// and the digest of a random image pushed as myorg/app:v1.
func fakeRegistry(t *testing.T) (host string, digest v1.Hash, srv *httptest.Server) {
	t.Helper()
	srv = httptest.NewServer(regfake.New(regfake.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	img, err := random.Image(1024, 1)
	if err != nil {
		t.Fatal(err)
	}
	tag, err := name.ParseReference(u.Host + "/myorg/app:v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(tag, img); err != nil {
		t.Fatal(err)
	}
	digest, err = img.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return u.Host, digest, srv
}

func TestResolveTag(t *testing.T) {
	host, digest, _ := fakeRegistry(t)
	got, err := New(0).Resolve(context.Background(), host+"/myorg/app:v1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != digest.Hex {
		t.Fatalf("Resolve = %q, want %q", got, digest.Hex)
	}
}

func TestResolveCachesByRef(t *testing.T) {
	host, digest, srv := fakeRegistry(t)
	r := New(time.Minute)
	first, err := r.Resolve(context.Background(), host+"/myorg/app:v1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	srv.Close() // second lookup must be served from cache
	second, err := r.Resolve(context.Background(), host+"/myorg/app:v1")
	if err != nil {
		t.Fatalf("cached Resolve: %v", err)
	}
	if first != digest.Hex || second != digest.Hex {
		t.Fatalf("got %q then %q, want %q", first, second, digest.Hex)
	}
}

func TestResolveUnknownTagFails(t *testing.T) {
	host, _, _ := fakeRegistry(t)
	if _, err := New(0).Resolve(context.Background(), host+"/myorg/app:nope"); err == nil {
		t.Fatal("expected error for unknown tag")
	}
}

func TestResolvePinnedImageSkipsNetwork(t *testing.T) {
	digest := strings.Repeat("b", 64)
	// unroutable registry host: must not be contacted for a pinned ref
	got, err := New(0).Resolve(context.Background(), "no.such.registry.invalid/app@sha256:"+digest)
	if err != nil || got != digest {
		t.Fatalf("Resolve = %q, %v; want %q", got, err, digest)
	}
}

func TestResolveInvalidReference(t *testing.T) {
	for _, img := range []string{"not@@valid", "", "UPPER CASE"} {
		if _, err := New(0).Resolve(context.Background(), img); err == nil {
			t.Errorf("Resolve(%q): expected error", img)
		}
	}
}

func TestResolveRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(0).Resolve(ctx, "no.such.registry.invalid/app:v1"); err == nil {
		t.Fatal("expected error with canceled context")
	}
}

// TestLiveResolution talks to real public registries; opt in with
// RESOLVE_LIVE_TEST=1 (requires network).
func TestLiveResolution(t *testing.T) {
	if os.Getenv("RESOLVE_LIVE_TEST") != "1" {
		t.Skip("set RESOLVE_LIVE_TEST=1 to run against real registries")
	}
	r := New(time.Minute)
	for _, img := range []string{"busybox:latest", "nginx", "ghcr.io/fluxcd/source-controller:v1.3.0"} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		d, err := r.Resolve(ctx, img)
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", img, err)
		}
		if len(d) != 64 {
			t.Fatalf("%s: digest %q is not 64 hex chars", img, d)
		}
		t.Logf("%s -> sha256:%s", img, d)
	}
}
