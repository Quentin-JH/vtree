package naming

import "testing"

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"crew-credit":     "crew_credit",
		"113-surface-422": "113_surface_422",
		"Design.Engine":   "design_engine",
		"fix_297":         "fix_297",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSchemaName(t *testing.T) {
	if got := SchemaName("velera_", "crew-credit", "main"); got != "velera_crew_credit" {
		t.Errorf("main schema = %q", got)
	}
	if got := SchemaName("velera_", "crew-credit", "test"); got != "velera_crew_credit_test" {
		t.Errorf("test schema = %q", got)
	}
}

func TestSchemaCollisionPairExists(t *testing.T) {
	// The X / X-test trap: these two distinct tree names derive the same
	// schema. The collision check in `up` exists because of this.
	a := SchemaName("velera_", "fix-reporting", "test")
	b := SchemaName("velera_", "fix-reporting-test", "main")
	if a != b {
		t.Fatalf("expected the known collision, got %q vs %q — if naming changed, update the up collision check too", a, b)
	}
}

func TestValidateTreeName(t *testing.T) {
	for _, ok := range []string{"crew-credit", "fix.297", "a", "113-x"} {
		if err := ValidateTreeName(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a/b", "a b", "-lead", ".hidden", "semi;colon"} {
		if err := ValidateTreeName(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
