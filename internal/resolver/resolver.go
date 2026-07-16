// Package resolver resolves image references (repo:tag) to sha256 digests
// by asking the image registry, via a manifest HEAD request against the
// OCI Distribution API. Results are cached with a TTL because tags are
// mutable.
//
// Authentication uses the Docker keychain (~/.docker/config.json or
// DOCKER_CONFIG), falling back to anonymous access — public images work
// out of the box; for private registries the chart's
// webhook.registryCredentialsSecret mounts a Docker config Secret.
package resolver

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type Resolver struct {
	ttl   time.Duration
	mu    sync.RWMutex
	cache map[string]entry
}

// cache entries are only replaced on overwrite; the map is bounded by the
// number of distinct image refs admitted, which is small in practice.
type entry struct {
	digest  string
	expires time.Time
}

func New(ttl time.Duration) *Resolver {
	return &Resolver{ttl: ttl, cache: map[string]entry{}}
}

// Resolve returns the bare hex sha256 digest for an image reference,
// querying the registry when the reference is not already digest-pinned.
func (r *Resolver) Resolve(ctx context.Context, image string) (string, error) {
	ref, err := name.ParseReference(image)
	if err != nil {
		return "", fmt.Errorf("invalid image reference %q: %w", image, err)
	}
	if d, ok := ref.(name.Digest); ok {
		ds := d.DigestStr()
		if !strings.HasPrefix(ds, "sha256:") {
			return "", fmt.Errorf("image %q: unsupported digest algorithm (want sha256)", image)
		}
		return strings.TrimPrefix(ds, "sha256:"), nil
	}

	// ref.Name() is fully qualified (index.docker.io/library/busybox:latest),
	// so equivalent spellings of the same image share one cache entry.
	key := ref.Name()
	if r.ttl > 0 {
		r.mu.RLock()
		e, ok := r.cache[key]
		r.mu.RUnlock()
		if ok && time.Now().Before(e.expires) {
			return e.digest, nil
		}
	}

	digest, err := r.lookup(ctx, ref)
	if err != nil {
		return "", err
	}
	if r.ttl > 0 {
		r.mu.Lock()
		r.cache[key] = entry{digest: digest, expires: time.Now().Add(r.ttl)}
		r.mu.Unlock()
	}
	return digest, nil
}

func (r *Resolver) lookup(ctx context.Context, ref name.Reference) (string, error) {
	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}
	// HEAD first: a few KB of manifest metadata, no blobs. Some registries
	// omit the digest header on HEAD, so fall back to GET, which digests
	// the manifest body itself.
	var digest string
	if desc, err := remote.Head(ref, opts...); err == nil {
		digest = desc.Digest.String()
	} else if full, gerr := remote.Get(ref, opts...); gerr == nil {
		digest = full.Digest.String()
	} else {
		return "", fmt.Errorf("registry lookup for %q failed: %w", ref.String(), gerr)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		return "", fmt.Errorf("registry returned unsupported digest %q for %q", digest, ref.String())
	}
	return strings.TrimPrefix(digest, "sha256:"), nil
}
