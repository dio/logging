package gcp

import (
	"context"
	"os"
	"sync"
	"time"

	"cloud.google.com/go/compute/metadata"
)

// metadataTimeout caps the metadata server fetch. 200ms is generous on GCP
// (typical responses are sub-10ms) and short enough that non-GCP environments
// barely notice the one-time miss.
const metadataTimeout = 200 * time.Millisecond

// projectFetcher fetches the project ID from the GCP metadata server. It is
// a package var so tests can swap it without going through the
// cloud.google.com/go/compute/metadata package's own caching layers.
var projectFetcher = func(ctx context.Context) (string, error) {
	return metadata.NewClient(nil).ProjectIDWithContext(ctx)
}

var (
	projectOnce sync.Once
	projectVal  string
)

// ResolveProjectID returns the GCP project ID using the standard chain:
//
//  1. explicit value (if non-empty), returned as-is
//  2. GOOGLE_CLOUD_PROJECT env (App Engine, Cloud Functions, gcloud CLI)
//  3. GCLOUD_PROJECT env (legacy)
//  4. GCP metadata server via cloud.google.com/go/compute/metadata
//     (Cloud Run, GKE, GCE, App Engine flex), 200ms timeout, cached
//     for the process lifetime
//
// On non-GCP environments steps 2-4 fail and ResolveProjectID returns "".
// Callers should pass the result into NewHandler; an empty value disables
// the trace correlation rewrite (logs still emit valid Cloud Logging JSON,
// they just do not link to Cloud Trace).
//
// The metadata fetch happens at most once per process. Both success and
// failure are cached so non-GCP environments do not pay the timeout
// repeatedly.
func ResolveProjectID(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("GOOGLE_CLOUD_PROJECT"); v != "" {
		return v
	}
	if v := os.Getenv("GCLOUD_PROJECT"); v != "" {
		return v
	}
	return detectProjectFromMetadata()
}

// detectProjectFromMetadata queries the GCP metadata server once per
// process and caches the result (success or failure). The actual fetch is
// delegated to projectFetcher, which by default uses the official
// cloud.google.com/go/compute/metadata client.
func detectProjectFromMetadata() string {
	projectOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), metadataTimeout)
		defer cancel()

		v, err := projectFetcher(ctx)
		if err != nil {
			return
		}
		projectVal = v
	})
	return projectVal
}

// resetProjectCacheForTests clears the once-cached metadata response.
// Test-only helper; not part of the public API.
func resetProjectCacheForTests() {
	projectOnce = sync.Once{}
	projectVal = ""
}

// setProjectFetcherForTests substitutes the metadata fetcher used by the
// detection path. Pass nil to restore the default.
func setProjectFetcherForTests(f func(context.Context) (string, error)) {
	if f == nil {
		projectFetcher = func(ctx context.Context) (string, error) {
			return metadata.NewClient(nil).ProjectIDWithContext(ctx)
		}
		return
	}
	projectFetcher = f
}
