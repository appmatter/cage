package vminstance

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	for input, want := range map[string]string{
		"":           "default",
		"feature-01": "feature-01",
		" task_1 ":   "task_1",
	} {
		got, err := Normalize(input)
		if err != nil || got != want {
			t.Fatalf("%q: got %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"-bad", "has space", "../escape"} {
		if _, err := Normalize(input); err == nil {
			t.Fatalf("%q accepted", input)
		}
	}
}

func TestResolveNamespacesBackendID(t *testing.T) {
	instanceID, first, err := Resolve("/project/a", "/project/a/.cage/cage.yaml", "default")
	if err != nil || instanceID != "default" || len(first) != 29 || !strings.HasPrefix(first, "cage-") {
		t.Fatalf("first: instanceID=%q backendVMID=%q err=%v", instanceID, first, err)
	}
	_, again, err := Resolve("/project/a", "/project/a/.cage/cage.yaml", "default")
	if err != nil || first != again {
		t.Fatalf("unstable ID: %q %q %v", first, again, err)
	}
	_, otherProject, err := Resolve("/project/b", "/project/b/.cage/cage.yaml", "default")
	if err != nil || first == otherProject {
		t.Fatalf("project collision: %q %q %v", first, otherProject, err)
	}
	_, otherConfig, err := Resolve("/project/a", "/project/a/.cage/cage.dogfood.yaml", "default")
	if err != nil || first == otherConfig {
		t.Fatalf("config collision: %q %q %v", first, otherConfig, err)
	}
	_, otherInstance, err := Resolve("/project/a", "/project/a/.cage/cage.yaml", "feature-01")
	if err != nil || first == otherInstance {
		t.Fatalf("instance collision: %q %q %v", first, otherInstance, err)
	}
}
