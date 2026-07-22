package containers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJunocashdBuildContextIncludesDockerfile(t *testing.T) {
	contextDir := junocashdBuildContext()
	dockerfile := filepath.Join(contextDir, "Dockerfile")

	info, err := os.Stat(contextDir)
	if err != nil {
		t.Fatalf("stat build context: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("build context %q is not a directory", contextDir)
	}

	raw, err := os.ReadFile(dockerfile)
	if err != nil {
		t.Fatalf("read Dockerfile from build context: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("Dockerfile is empty")
	}
}
