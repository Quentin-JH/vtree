package manifest

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSetDisplayNameRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "t1")
	if err := Claim(dir, &Manifest{Name: "t1", Ports: map[string]int{"api": 4000}}); err != nil {
		t.Fatal(err)
	}
	if err := SetDisplayName(dir, "Crew Contribution Tracking"); err != nil {
		t.Fatal(err)
	}
	m, err := Read(dir)
	if err != nil || m.DisplayName != "Crew Contribution Tracking" {
		t.Fatalf("display name not persisted: %+v %v", m, err)
	}
	if m.Ports["api"] != 4000 {
		t.Error("rename must not lose other manifest fields")
	}
	// Clearing works too.
	if err := SetDisplayName(dir, ""); err != nil {
		t.Fatal(err)
	}
	m, _ = Read(dir)
	if m.DisplayName != "" {
		t.Error("display name not cleared")
	}
}

func TestSetDisplayNameNeedsManifest(t *testing.T) {
	err := SetDisplayName(t.TempDir(), "x")
	if err == nil || !strings.Contains(err.Error(), "adopt") {
		t.Errorf("manifest-less tree should be refused with the adopt remedy: %v", err)
	}
}
