package server

import (
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
)

func TestBuildInfoFallback(t *testing.T) {
	for _, key := range []string{"APP_VERSION", "GIT_SHA"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	version(w, httptest.NewRequest("GET", "/version", nil))
	want := fmt.Sprintf("VERSION=%s\nGIT_SHA=%s", buildVersion, buildCommit)
	if w.Body.String() != want {
		t.Fatalf("version response = %q, want %q", w.Body.String(), want)
	}
	t.Log(w.Body.String())
}

func TestBuildInfoRuntimeOverrides(t *testing.T) {
	for _, values := range [][2]string{{"custom-version", "custom-commit"}, {"", ""}} {
		t.Run(fmt.Sprintf("%q", values[0]), func(t *testing.T) {
			t.Setenv("APP_VERSION", values[0])
			t.Setenv("GIT_SHA", values[1])
			version, commit := buildInfo()
			if version != values[0] || commit != values[1] {
				t.Fatalf("runtime overrides not preserved: %q %q", version, commit)
			}
		})
	}
}
