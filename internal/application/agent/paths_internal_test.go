package agent

import "testing"

// TestIsPlanArtifactPathSeparators cubre el bug de Windows: filepath.Clean
// devuelve `\` ahí, y la comparación con "/" literales fallaba (el modo plan
// denegaba hasta el propio plan + el test de gate fallaba en CI windows).
func TestIsPlanArtifactPathSeparators(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{".forgen/plans/plan.md", true},
		{".forgen/plans/otro.md", true},
		{".forgen/plans/sub/plan.md", true},
		{`.forgen\plans\plan.md`, true},   // filepath.Clean en Windows
		{`.forgen\plans\otro.md`, true},   // filepath.Clean en Windows
		{"src/main.go", false},
		{".forgen/memory.md", false},
		{".forgen/plans", false},          // el directorio no es artefacto
		{".forgen/plansfake/x.md", false}, // prefijo parecido pero distinto dir
	}
	for _, tc := range cases {
		if got := isPlanArtifactPath(tc.path); got != tc.want {
			t.Errorf("isPlanArtifactPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
