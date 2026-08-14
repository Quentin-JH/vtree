// Package naming derives MySQL schema names from free-form tree names.
package naming

import (
	"fmt"
	"regexp"
	"strings"
)

// treeNameRe permits names usable as a directory, a branch segment, and a
// schema-slug source. No slashes, no spaces, nothing shell-hostile.
var treeNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func ValidateTreeName(name string) error {
	if !treeNameRe.MatchString(name) {
		return fmt.Errorf("invalid tree name %q — use letters, digits, dots, dashes, underscores; it becomes a directory, a branch name, and a schema slug", name)
	}
	return nil
}

// Slug maps a tree name to a MySQL-identifier-safe fragment: lowercase, every
// non-alphanumeric becomes an underscore. Tree names routinely contain
// characters MySQL rejects in unquoted identifiers (design-engine,
// 113-surface-422). The workspace prefix guarantees the full name never
// starts with a digit.
func Slug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

// SchemaName builds the full schema name for one of the workspace's declared
// schemas. The schema named "main" is the bare prefixed slug; every other
// schema is suffixed: (velera_, crew-credit, main) → velera_crew_credit,
// (velera_, crew-credit, test) → velera_crew_credit_test.
func SchemaName(prefix, treeName, schema string) string {
	base := prefix + Slug(treeName)
	if schema == "main" {
		return base
	}
	return base + "_" + schema
}

// SchemaSet returns schema-name → full MySQL schema name for a tree.
func SchemaSet(prefix, treeName string, schemas []string) map[string]string {
	out := make(map[string]string, len(schemas))
	for _, s := range schemas {
		out[s] = SchemaName(prefix, treeName, s)
	}
	return out
}
