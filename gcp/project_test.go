package gcp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubFetcher returns a fetcher that records call count and returns the
// supplied value/error.
func stubFetcher(t *testing.T, val string, err error) (call func() int) {
	t.Helper()
	var calls int
	setProjectFetcherForTests(func(_ context.Context) (string, error) {
		calls++
		return val, err
	})
	t.Cleanup(func() { setProjectFetcherForTests(nil) })
	return func() int { return calls }
}

func TestResolveProjectID_explicitWins(t *testing.T) {
	resetProjectCacheForTests()
	t.Setenv("GOOGLE_CLOUD_PROJECT", "from-env")

	got := ResolveProjectID("explicit-project")
	if got != "explicit-project" {
		t.Errorf("explicit project should win, got %q", got)
	}
}

func TestResolveProjectID_googleCloudProjectEnv(t *testing.T) {
	resetProjectCacheForTests()
	t.Setenv("GOOGLE_CLOUD_PROJECT", "from-env")
	t.Setenv("GCLOUD_PROJECT", "")

	got := ResolveProjectID("")
	if got != "from-env" {
		t.Errorf("want from-env, got %q", got)
	}
}

func TestResolveProjectID_gcloudProjectFallback(t *testing.T) {
	resetProjectCacheForTests()
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GCLOUD_PROJECT", "legacy-env")

	got := ResolveProjectID("")
	if got != "legacy-env" {
		t.Errorf("want legacy-env, got %q", got)
	}
}

func TestResolveProjectID_metadataServerLastResort(t *testing.T) {
	resetProjectCacheForTests()
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GCLOUD_PROJECT", "")
	calls := stubFetcher(t, "metadata-detected-project", nil)

	got := ResolveProjectID("")
	if got != "metadata-detected-project" {
		t.Errorf("want metadata-detected-project, got %q", got)
	}
	if c := calls(); c != 1 {
		t.Errorf("fetcher called %d times, want 1", c)
	}
}

func TestResolveProjectID_metadataServerFailureCached(t *testing.T) {
	resetProjectCacheForTests()
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GCLOUD_PROJECT", "")
	calls := stubFetcher(t, "", errors.New("metadata unreachable"))

	for range 3 {
		if got := ResolveProjectID(""); got != "" {
			t.Errorf("want empty (failure cached), got %q", got)
		}
	}
	if c := calls(); c != 1 {
		t.Errorf("fetcher called %d times across 3 ResolveProjectID calls, want 1 (failure must be cached)", c)
	}
}

func TestResolveProjectID_metadataSuccessCachedAcrossCalls(t *testing.T) {
	resetProjectCacheForTests()
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GCLOUD_PROJECT", "")
	calls := stubFetcher(t, "cached-project", nil)

	for range 5 {
		if got := ResolveProjectID(""); got != "cached-project" {
			t.Errorf("want cached-project, got %q", got)
		}
	}
	if c := calls(); c != 1 {
		t.Errorf("fetcher called %d times across 5 ResolveProjectID calls, want 1 (success must be cached)", c)
	}
}

func TestNewHandler_emptyProjectAutoDetects(t *testing.T) {
	resetProjectCacheForTests()
	t.Setenv("GOOGLE_CLOUD_PROJECT", "auto-test-project")

	var buf strings.Builder
	h := NewHandler(&buf, "", nil)
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
	// Handler is opaque; ResolveProjectID tests above cover the resolution logic.
	// This test guards against the empty-string code path crashing.
}
