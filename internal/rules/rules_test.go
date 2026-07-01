package rules

import (
	"reflect"
	"strings"
	"testing"
)

func TestPacksEmbedded(t *testing.T) {
	for _, name := range PackNames() {
		if !ValidPack(name) {
			t.Errorf("pack %s not valid", name)
		}
		if len(strings.TrimSpace(packContent[name])) == 0 {
			t.Errorf("pack %s is empty", name)
		}
	}
	if ValidPack("nonsense") {
		t.Error("ValidPack should reject unknown names")
	}
}

func TestDetectPacks(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
		want  []string
	}{
		{"go only", []string{"main.go", "go.mod"}, []string{PackCore}},
		{"python", []string{"app/views.py"}, []string{PackCore, PackPythonBackend}},
		{"plain ts", []string{"src/lib/api.ts", "src/util.js"}, []string{PackCore, PackTypeScript}},
		{"react tsx", []string{"src/Button.tsx"}, []string{PackCore, PackTypeScript, PackReact}},
		{"jsx", []string{"src/App.jsx"}, []string{PackCore, PackTypeScript, PackReact}},
		{"mixed stack", []string{"api/views.py", "web/src/Page.tsx"},
			[]string{PackCore, PackPythonBackend, PackTypeScript, PackReact}},
		{"case insensitive", []string{"src/App.TSX"}, []string{PackCore, PackTypeScript, PackReact}},
	}
	for _, tc := range cases {
		if got := DetectPacks(tc.paths); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: DetectPacks = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNormalizePacks(t *testing.T) {
	got := NormalizePacks([]string{PackReact, PackTypeScript, PackReact})
	want := []string{PackCore, PackTypeScript, PackReact}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NormalizePacks = %v, want %v (core forced, deduped, ordered)", got, want)
	}
}

func TestBuildUserPromptIncludesPacks(t *testing.T) {
	files := []FileDiff{{Path: "src/Button.tsx", Patch: "+const x = 1"}}
	prompt := BuildUserPrompt([]string{PackCore, PackTypeScript, PackReact}, []string{"Repo rule."}, files, nil)

	for _, want := range []string{
		"# Generic review rules",
		"# JavaScript / TypeScript rules",
		"# React rules",
		"Design system",
		"# Repo-specific rules",
		"- Repo rule.",
		"## File: src/Button.tsx",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if strings.Contains(prompt, "# Python backend rules") {
		t.Error("prompt should not include unselected packs")
	}
}
