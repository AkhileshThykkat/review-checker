package llm

import "testing"

func TestParseFindingsPlainArray(t *testing.T) {
	in := `[{"file":"app/views.py","line":12,"severity":"block","comment":"raw SQL"}]`
	got, err := ParseFindings(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].File != "app/views.py" || got[0].Line != 12 || got[0].Severity != "block" {
		t.Errorf("unexpected findings: %+v", got)
	}
}

func TestParseFindingsCodeFence(t *testing.T) {
	in := "```json\n[{\"file\":\"a.py\",\"line\":3,\"severity\":\"nit\",\"comment\":\"x\"}]\n```"
	got, err := ParseFindings(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Line != 3 {
		t.Errorf("unexpected findings: %+v", got)
	}
}

func TestParseFindingsProseWrapped(t *testing.T) {
	in := `Here are my findings:
[{"file":"a.py","line":1,"severity":"warn","comment":"y"}]
Let me know if you need more.`
	got, err := ParseFindings(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("unexpected findings: %+v", got)
	}
}

func TestParseFindingsEmptyArray(t *testing.T) {
	got, err := ParseFindings("[]")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want no findings, got %+v", got)
	}
}

func TestParseFindingsDropsInvalidAndDefaultsSeverity(t *testing.T) {
	in := `[
		{"file":"","line":1,"severity":"warn","comment":"no file"},
		{"file":"a.py","line":0,"severity":"warn","comment":"bad line"},
		{"file":"a.py","line":2,"severity":"","comment":"no severity"},
		{"file":"a.py","line":3,"severity":"critical","comment":"unknown severity"}
	]`
	got, err := ParseFindings(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 valid findings, got %+v", got)
	}
	for _, f := range got {
		if f.Severity != SeverityWarn {
			t.Errorf("severity should default to warn, got %q", f.Severity)
		}
	}
}

func TestParseFindingsNormalizesPaths(t *testing.T) {
	in := `[
		{"file":"./a.py","line":1,"severity":"warn","comment":"dot-slash prefix"},
		{"file":"/b.py","line":2,"severity":"warn","comment":"slash prefix"},
		{"file":"c.py","line":3,"severity":"warn","comment":"already clean"},
		{"file":"./","line":4,"severity":"warn","comment":"empty after trim"}
	]`
	got, err := ParseFindings(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.py", "b.py", "c.py"}
	if len(got) != len(want) {
		t.Fatalf("want %d findings, got %+v", len(want), got)
	}
	for i, f := range got {
		if f.File != want[i] {
			t.Errorf("file[%d] = %q, want %q", i, f.File, want[i])
		}
	}
}

func TestParseFindingsGarbage(t *testing.T) {
	if _, err := ParseFindings("I could not review this."); err == nil {
		t.Error("want error for response without JSON array")
	}
}
