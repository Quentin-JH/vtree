// Package ports allocates the lowest free block of ports at or above the
// workspace base.
//
// "Used" is derived from what exists, in three layers:
//
//  1. Every tree manifest's recorded ports.
//  2. For trees WITHOUT a manifest (pre-vtree trees, or a manifest lost
//     mid-flight): the rendered .env values of every key whose template
//     references a ${VTREE_PORT_*} variable. Without this fallback, a
//     stopped legacy tree's ports would be re-issued and the two trees
//     would collide the next time both ran their dev servers.
//  3. Anything currently listening — probed by binding, not by lsof, so it
//     works on machines that don't have lsof.
package ports

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Quentin-JH/vtree/internal/config"
	"github.com/Quentin-JH/vtree/internal/manifest"
)

// Allocate returns name→port for cfg.Ports.Names, claiming the lowest block
// of len(names) consecutive ports at or above base that no tree claims and
// nothing is listening on.
func Allocate(treesDir string, cfg *config.Config) (map[string]int, error) {
	if cfg.Ports == nil {
		return nil, nil
	}
	used, err := usedPorts(treesDir, cfg)
	if err != nil {
		return nil, err
	}
	step := len(cfg.Ports.Names)
	for candidate := cfg.Ports.Base; candidate < 65536-step; candidate += step {
		ok := true
		for p := candidate; p < candidate+step; p++ {
			if used[p] || listening(p) {
				ok = false
				break
			}
		}
		if ok {
			out := make(map[string]int, step)
			for i, name := range cfg.Ports.Names {
				out[name] = candidate + i
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("no free port block of %d found above %d", step, cfg.Ports.Base)
}

func usedPorts(treesDir string, cfg *config.Config) (map[int]bool, error) {
	used := map[int]bool{}
	entries, err := os.ReadDir(treesDir)
	if os.IsNotExist(err) {
		return used, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		treeDir := filepath.Join(treesDir, e.Name())
		m, err := manifest.Read(treeDir)
		if err != nil {
			// A corrupt manifest must not make its ports invisible — that
			// would hand them out again. Fail the allocation instead.
			return nil, fmt.Errorf("cannot read manifest of tree %q: %w", e.Name(), err)
		}
		if m != nil {
			for _, p := range m.Ports {
				used[p] = true
			}
			continue
		}
		for _, p := range EnvPorts(treeDir, cfg) {
			used[p] = true
		}
	}
	return used, nil
}

// EnvPorts extracts port values from a tree's rendered env files by reading
// the keys that the workspace's env templates fill with ${VTREE_PORT_*}
// variables. The rendered .env is what the app actually binds, so this is the
// truth manifests are reconciled against.
func EnvPorts(treeDir string, cfg *config.Config) []int {
	var out []int
	for _, repo := range cfg.Repos {
		for _, ef := range repo.EnvFiles {
			var portKeys []string
			for _, k := range ef.Set.Keys {
				if strings.Contains(ef.Set.Values[k], "${VTREE_PORT_") {
					portKeys = append(portKeys, k)
				}
			}
			if len(portKeys) == 0 {
				continue
			}
			data, err := os.ReadFile(filepath.Join(treeDir, repo.Name, ef.Dest))
			if err != nil {
				continue
			}
			values := envValues(string(data), portKeys)
			for _, v := range values {
				for _, p := range extractPorts(v) {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

func envValues(content string, keys []string) []string {
	var out []string
	for _, line := range strings.Split(content, "\n") {
		for _, k := range keys {
			if v, ok := strings.CutPrefix(line, k+"="); ok {
				out = append(out, v)
			}
		}
	}
	return out
}

// extractPorts pulls port numbers out of an env value, which may be a bare
// number ("4100") or a URL ("http://localhost:4100/api").
func extractPorts(v string) []int {
	var out []int
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			continue
		}
		j := i
		for j < len(v) && v[j] >= '0' && v[j] <= '9' {
			j++
		}
		// A port is either the whole value or preceded by ':'.
		if i == 0 && j == len(v) || i > 0 && v[i-1] == ':' {
			if n, err := strconv.Atoi(v[i:j]); err == nil && n >= 1024 && n < 65536 {
				out = append(out, n)
			}
		}
		i = j
	}
	return out
}

// listening reports whether something already listens on the port, by trying
// to bind it on all interfaces.
func listening(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return true
	}
	l.Close()
	return false
}
