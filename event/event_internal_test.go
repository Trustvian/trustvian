package event

import "testing"

// TestTargetCategoryValid exercises the unexported valid() method directly.
// This lives in a white-box (package event) test file, unlike the rest of
// this package's tests (event_test.go, package event_test), specifically
// because valid() has no other in-package caller to reach it: unlike
// ActorType/OperationCategory/OperationDirection's valid() methods, which
// are exercised transitively through Validate(), TargetCategory.valid() is
// deliberately never called from Validate() (see Target's doc comment) —
// a direct test is its only exercise.
func TestTargetCategoryValid(t *testing.T) {
	tests := []struct {
		name string
		cat  TargetCategory
		want bool
	}{
		{"unspecified is valid", TargetCategoryUnspecified, true},
		{"internal", TargetCategoryInternal, true},
		{"external", TargetCategoryExternal, true},
		{"database", TargetCategoryDatabase, true},
		{"unknown value invalid", TargetCategory("bogus"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cat.valid(); got != tt.want {
				t.Errorf("TargetCategory(%q).valid() = %v, want %v", tt.cat, got, tt.want)
			}
		})
	}
}
