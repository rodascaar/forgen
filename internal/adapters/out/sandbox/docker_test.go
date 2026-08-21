package sandbox_test

import (
	"testing"

	"github.com/rodascaar/forgen/internal/adapters/out/sandbox"
)

func TestDockerArgsConstruction(t *testing.T) {
	executor := sandbox.NewDockerExecutor("alpine:3.20", "/home/user/project")

	args := executor.BuildArgs("go test ./...")
	if len(args) == 0 {
		t.Fatal("args vacíos")
	}
	if args[0] != "run" {
		t.Fatalf("primer arg = %q, want run", args[0])
	}
	joined := joinArgs(args)
	for _, want := range []string{"--rm", "--network none", "/home/user/project:/workspace", "alpine:3.20", "sh", "-c"} {
		if !contains(joined, want) {
			t.Fatalf("args %v no contienen %q", args, want)
		}
	}
}

func joinArgs(args []string) string {
	result := ""
	for _, arg := range args {
		result += arg + " "
	}
	return result
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
