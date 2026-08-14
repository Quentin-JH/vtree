// Package config loads and validates the two vtree configuration files:
//
//   - .vtree/vtree.yaml — the workspace definition, committed and shared.
//   - .vtree/local.yaml — machine-specific settings (database credentials,
//     doctor hints), gitignored. Required before any database operation: vtree
//     never bakes in connection defaults, so it can never fire CREATE DATABASE
//     at a server nobody configured.
//
// Both files are parsed strictly: an unknown key is an error, not a silent
// no-op. A misspelled key in a safety-relevant setting (ignore_dirty,
// guard_against) that parsed cleanly would weaken a guard without anyone
// noticing.
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Builtins are command names reserved by vtree itself. Custom commands may not
// shadow them: `vtree test` must never silently mean something other than what
// the workspace defined, and a builtin must never be unreachable.
var Builtins = []string{
	"adopt", "doctor", "down", "help", "init", "install",
	"ls", "pr", "rename", "run", "status", "ui", "up", "version",
}

type Config struct {
	Name        string     `yaml:"name"`
	Repos       []Repo     `yaml:"repos"`
	Ports       *Ports     `yaml:"ports"`
	Database    *Database  `yaml:"database"`
	Setup       string     `yaml:"setup"`
	Templates   []Template `yaml:"templates"`
	Commands    []Command  `yaml:"commands"`
	IgnoreDirty []string   `yaml:"ignore_dirty"`
	PR          *PR        `yaml:"pr"`
}

type Repo struct {
	Name         string    `yaml:"name"`
	Git          string    `yaml:"git"`
	BaseRef      string    `yaml:"base_ref"`
	BranchPrefix string    `yaml:"branch_prefix"`
	GitHooks     []string  `yaml:"git_hooks"`
	EnvFiles     []EnvFile `yaml:"env_files"`
}

type EnvFile struct {
	Source   string   `yaml:"source"`
	Dest     string   `yaml:"dest"`
	Optional bool     `yaml:"optional"`
	Delete   []string `yaml:"delete"`
	Set      EnvSet   `yaml:"set"`
}

// EnvSet is an insertion-ordered key→value map. Order matters: rendered .env
// files must come out byte-deterministic (the migration validates bash-vtree
// output against Go-vtree output with a diff), and Go's map iteration is
// deliberately randomized.
type EnvSet struct {
	Keys   []string
	Values map[string]string
}

func (s *EnvSet) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: set must be a mapping of KEY: value", node.Line)
	}
	s.Values = map[string]string{}
	for i := 0; i < len(node.Content); i += 2 {
		k, v := node.Content[i], node.Content[i+1]
		if _, dup := s.Values[k.Value]; dup {
			return fmt.Errorf("line %d: duplicate set key %q", k.Line, k.Value)
		}
		s.Keys = append(s.Keys, k.Value)
		s.Values[k.Value] = v.Value
	}
	return nil
}

type Ports struct {
	Base  int      `yaml:"base"`
	Names []string `yaml:"names"`
}

type Database struct {
	Prefix  string   `yaml:"prefix"`
	Schemas []string `yaml:"schemas"`
}

type Template struct {
	Source string `yaml:"source"`
	Dest   string `yaml:"dest"`
}

type Command struct {
	Name    string `yaml:"name"`
	Command string `yaml:"command"`
	Scope   string `yaml:"scope"`
}

type PR struct {
	Base string `yaml:"base"`
	// GuardAgainst names the branches a feature might have been wrongly cut
	// from (e.g. main when PRs target staging). The stray-commit guard needs
	// this third ref: comparing only against the PR base is vacuous, because
	// merge-base(base, HEAD) is an ancestor of base by construction. Empty
	// means the drift check is skipped, not weakened.
	GuardAgainst []string `yaml:"guard_against"`
}

// Local is .vtree/local.yaml — everything that is true of this machine rather
// than of the workspace.
type Local struct {
	Database *LocalDatabase    `yaml:"database"`
	Hints    map[string]string `yaml:"hints"`
}

type LocalDatabase struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

var (
	identRe  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	schemaRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Load reads, strictly parses, and validates a vtree.yaml.
func Load(path string) (*Config, error) {
	var cfg Config
	if err := decodeStrict(path, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.validate(path); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	return &cfg, nil
}

// LoadLocal reads and strictly parses a local.yaml. The file being absent is
// the caller's condition to handle (os.IsNotExist), not an error wrapped here.
func LoadLocal(path string) (*Local, error) {
	var loc Local
	if err := decodeStrict(path, &loc); err != nil {
		return nil, err
	}
	if db := loc.Database; db != nil {
		if db.Host == "" || db.Port == 0 || db.User == "" {
			return nil, fmt.Errorf("%s: database needs host, port, and user (password may be empty)", path)
		}
	}
	return &loc, nil
}

func decodeStrict(path string, out any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func (c *Config) applyDefaults() {
	for i := range c.Repos {
		if c.Repos[i].BranchPrefix == "" {
			c.Repos[i].BranchPrefix = "feature/"
		}
	}
}

func (c *Config) validate(path string) error {
	e := func(format string, a ...any) error {
		return fmt.Errorf("%s: %s", path, fmt.Sprintf(format, a...))
	}

	if c.Name == "" {
		return e(`missing required field "name"`)
	}
	if len(c.Repos) == 0 {
		return e("at least one repo is required")
	}

	seenRepo := map[string]bool{}
	for i, r := range c.Repos {
		at := func(format string, a ...any) error {
			return e("repos[%d] (%s): %s", i, orPlaceholder(r.Name), fmt.Sprintf(format, a...))
		}
		if r.Name == "" {
			return at(`missing required field "name"`)
		}
		if seenRepo[r.Name] {
			return at("duplicate repo name")
		}
		seenRepo[r.Name] = true
		if r.Git == "" {
			return at(`missing required field "git"`)
		}
		if r.BaseRef == "" {
			return at(`missing required field "base_ref" — name the ref trees are cut from (e.g. origin/staging); vtree does not guess, because cutting from the wrong branch carries other people's commits into your PRs`)
		}
		for j, ef := range r.EnvFiles {
			if ef.Source == "" || ef.Dest == "" {
				return at(`env_files[%d]: "source" and "dest" are both required`, j)
			}
			for _, k := range ef.Set.Keys {
				if strings.TrimSpace(k) == "" {
					return at("env_files[%d]: set contains an empty key", j)
				}
			}
			for _, k := range ef.Delete {
				if strings.TrimSpace(k) == "" {
					return at("env_files[%d]: delete contains an empty key", j)
				}
			}
		}
	}

	if p := c.Ports; p != nil {
		if p.Base <= 0 {
			return e(`ports: "base" must be a positive port number`)
		}
		if len(p.Names) == 0 {
			return e(`ports: "names" must list at least one port name (e.g. [api, web])`)
		}
		seen := map[string]bool{}
		for _, n := range p.Names {
			if !identRe.MatchString(n) {
				return e("ports: name %q must be lowercase alphanumeric/underscore, starting with a letter (it becomes ${VTREE_PORT_%s})", n, strings.ToUpper(n))
			}
			if seen[n] {
				return e("ports: duplicate name %q", n)
			}
			seen[n] = true
		}
	}

	if d := c.Database; d != nil {
		if d.Prefix == "" {
			return e(`database: missing required field "prefix" — schemas are named <prefix><tree-slug>, and the prefix guarantees the name never starts with a digit`)
		}
		if len(d.Schemas) == 0 {
			return e(`database: "schemas" must list at least one schema name (e.g. [main, test])`)
		}
		seen := map[string]bool{}
		for _, s := range d.Schemas {
			if !schemaRe.MatchString(s) {
				return e("database: schema name %q must be lowercase alphanumeric/underscore, starting with a letter", s)
			}
			if seen[s] {
				return e("database: duplicate schema name %q", s)
			}
			seen[s] = true
		}
	}

	builtin := map[string]bool{}
	for _, b := range Builtins {
		builtin[b] = true
	}
	seenCmd := map[string]bool{}
	for i, cmd := range c.Commands {
		at := func(format string, a ...any) error {
			return e("commands[%d] (%s): %s", i, orPlaceholder(cmd.Name), fmt.Sprintf(format, a...))
		}
		if cmd.Name == "" {
			return at(`missing required field "name"`)
		}
		if builtin[cmd.Name] {
			return at("shadows the built-in vtree command %q — pick another name", cmd.Name)
		}
		if seenCmd[cmd.Name] {
			return at("duplicate command name")
		}
		seenCmd[cmd.Name] = true
		if cmd.Command == "" {
			return at(`missing required field "command"`)
		}
		if cmd.Scope != "tree" && cmd.Scope != "workspace" {
			return at(`scope must be "tree" or "workspace", got %q`, cmd.Scope)
		}
	}

	for i, t := range c.Templates {
		if t.Source == "" || t.Dest == "" {
			return e(`templates[%d]: "source" and "dest" are both required`, i)
		}
	}

	for _, pat := range c.IgnoreDirty {
		if _, err := regexp.Compile(pat); err != nil {
			return e("ignore_dirty: %q is not a valid regular expression: %v", pat, err)
		}
	}

	if pr := c.PR; pr != nil {
		if pr.Base == "" {
			return e(`pr: missing required field "base"`)
		}
		for _, g := range pr.GuardAgainst {
			if g == pr.Base {
				return e("pr: guard_against contains the PR base %q itself — the drift check against the base is vacuous by construction; name the branch trees might be wrongly cut from instead (e.g. main)", g)
			}
		}
	}

	return nil
}

func orPlaceholder(s string) string {
	if s == "" {
		return "unnamed"
	}
	return s
}
