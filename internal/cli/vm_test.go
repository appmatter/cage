package cli

import (
	"strings"
	"testing"
)

func TestResolveInstanceID(t *testing.T) {
	for input, want := range map[string]string{
		"":           "default",
		"feature-01": "feature-01",
		" task_1 ":   "task_1",
	} {
		got, err := resolveInstanceID(input)
		if err != nil || got != want {
			t.Fatalf("%q: got %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"-bad", "has space", "../escape", ""} {
		if input == "" {
			continue
		}
		if _, err := resolveInstanceID(input); err == nil {
			t.Fatalf("%q accepted", input)
		}
	}
}

func TestResolveVMInstanceNamespacesBackendID(t *testing.T) {
	instanceID, first, err := resolveVMInstance("/project/a", "/project/a/.cage/cage.yaml", "default")
	if err != nil || instanceID != "default" || len(first) != 29 || !strings.HasPrefix(first, "cage-") {
		t.Fatalf("first: instanceID=%q backendVMID=%q err=%v", instanceID, first, err)
	}
	_, again, err := resolveVMInstance("/project/a", "/project/a/.cage/cage.yaml", "default")
	if err != nil || first != again {
		t.Fatalf("unstable ID: %q %q %v", first, again, err)
	}
	_, otherProject, err := resolveVMInstance("/project/b", "/project/b/.cage/cage.yaml", "default")
	if err != nil || first == otherProject {
		t.Fatalf("project collision: %q %q %v", first, otherProject, err)
	}
	_, otherConfig, err := resolveVMInstance("/project/a", "/project/a/.cage/cage.dogfood.yaml", "default")
	if err != nil || first == otherConfig {
		t.Fatalf("config collision: %q %q %v", first, otherConfig, err)
	}
	_, otherInstance, err := resolveVMInstance("/project/a", "/project/a/.cage/cage.yaml", "feature-01")
	if err != nil || first == otherInstance {
		t.Fatalf("instance collision: %q %q %v", first, otherInstance, err)
	}
}

func TestVMCommandsUseIDFlag(t *testing.T) {
	vm := newVMCmd()
	for _, command := range []string{"create", "start", "stop", "status", "delete", "exec", "logs"} {
		cmd, _, err := vm.Find([]string{command})
		if err != nil {
			t.Fatal(err)
		}
		if cmd.Flags().Lookup("id") == nil || cmd.Flags().Lookup("name") != nil {
			t.Fatalf("%s flags: %s", command, cmd.Flags().FlagUsages())
		}
	}
}
