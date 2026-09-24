package filter

import "testing"

func TestMatcherIncludeAndExcludePrecedence(t *testing.T) {
	matcher, err := New([]string{"**/*.xpp", "Models/**"}, []string{"**/bin/**", "**/*.dll"})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path string
		want bool
	}{
		{"Model/A.xpp", true},
		{"Models/readme.txt", true},
		{"Models/bin/generated.xpp", false},
		{"Model/library.dll", false},
		{"Model/readme.txt", false},
	}
	for _, test := range tests {
		if got := matcher.Include(test.path); got != test.want {
			t.Errorf("Include(%q) = %t, want %t", test.path, got, test.want)
		}
	}
	if !matcher.ExcludesDirectory("Model/bin") {
		t.Error("bin directory should be pruned")
	}
}
