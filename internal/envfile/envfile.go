// Package envfile renders .env files from a template file plus the config's
// delete/set directives.
//
// Semantics, in order:
//
//  1. delete — remove every line matching `^#? *KEY=` for each listed key.
//     Both live AND commented forms: .env.example files routinely ship one
//     backend's keys live and another's commented out (velera-crm ships
//     DB_CONNECTION=sqlite live with the MySQL keys commented). Upsert alone
//     would leave the commented duplicates behind, waiting for someone to
//     uncomment the wrong one.
//  2. set — for each key in author order: replace the first live `KEY=` line
//     in place, or append `KEY=value` at the end if no live line exists.
//
// Values may reference ${VTREE_*} variables. An unknown variable is an error,
// never an empty string — a typo'd ${VTREE_DB_PASS} silently expanding to ""
// would produce an .env that "works" against the wrong server.
package envfile

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Quentin-JH/vtree/internal/config"
)

var varRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Expand substitutes ${NAME} references from vars. Unknown names are errors.
// A var that is present but empty expands to "" — that is a configured value,
// not a mistake.
func Expand(s string, vars map[string]string) (string, error) {
	var missing []string
	out := varRe.ReplaceAllStringFunc(s, func(m string) string {
		name := varRe.FindStringSubmatch(m)[1]
		v, ok := vars[name]
		if !ok {
			missing = append(missing, name)
			return m
		}
		return v
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("unknown variable(s) %s in %q — available variables are the VTREE_* set for this tree", strings.Join(missing, ", "), s)
	}
	return out, nil
}

// Render applies ef's delete and set directives to the template content.
func Render(content string, ef config.EnvFile, vars map[string]string) (string, error) {
	// Split keeping structure; a trailing newline is restored at the end.
	trailingNL := strings.HasSuffix(content, "\n")
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")

	for _, key := range ef.Delete {
		re := keyLineRe(key)
		kept := lines[:0:0]
		for _, l := range lines {
			if !re.MatchString(l) {
				kept = append(kept, l)
			}
		}
		lines = kept
	}

	for _, key := range ef.Set.Keys {
		val, err := Expand(ef.Set.Values[key], vars)
		if err != nil {
			return "", fmt.Errorf("set %s: %w", key, err)
		}
		newLine := key + "=" + val
		re := liveKeyRe(key)
		replaced := false
		for i, l := range lines {
			if re.MatchString(l) {
				if !replaced {
					lines[i] = newLine
					replaced = true
				} else {
					// A second live occurrence of the same key would win in
					// most dotenv loaders; leaving it would silently undo the
					// set. Drop it.
					lines[i] = ""
				}
			}
		}
		if !replaced {
			lines = append(lines, newLine)
		}
	}

	out := strings.Join(lines, "\n")
	if trailingNL || len(ef.Set.Keys) > 0 {
		out += "\n"
	}
	return out, nil
}

func keyLineRe(key string) *regexp.Regexp {
	return regexp.MustCompile(`^#? *` + regexp.QuoteMeta(key) + `=`)
}

func liveKeyRe(key string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(key) + `=`)
}
