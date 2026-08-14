package tree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/Quentin-JH/vtree/internal/gitx"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

// Stray is one repo whose branch sits on the wrong base: it was forked from a
// guard_against branch that carries commits the PR base does not have, so a
// PR would carry those commits — which are not yours.
type Stray struct {
	Repo    string
	Branch  string
	Guard   string // the wrong-base candidate the fork was found on
	Fork    string // merge-base commit
	Commits int    // commits in pr.base..fork
}

// StrayCheck implements the drift guard. The check needs THREE refs: the PR
// base, HEAD, and the guard_against candidate — comparing HEAD against the PR
// base alone is vacuous, because merge-base(base, HEAD) is an ancestor of
// base by construction and the count is always zero. Remotes are fetched
// first; the guard is meaningless against stale remote-tracking refs.
func StrayCheck(ws *workspace.Workspace, name string) ([]Stray, error) {
	pr := ws.Config.PR
	if pr == nil {
		return nil, fmt.Errorf("no pr block in %s — set pr.base (and pr.guard_against) to use vtree pr", ws.ConfigPath())
	}
	var strays []Stray
	for _, repo := range ws.Config.Repos {
		wt := filepath.Join(ws.TreesPath(), name, repo.Name)
		if _, err := os.Stat(wt); err != nil {
			continue
		}
		if _, err := gitx.Run(wt, "fetch", "-q", "origin"); err != nil {
			return nil, err
		}
		branch, err := gitx.Run(wt, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return nil, err
		}
		for _, guard := range pr.GuardAgainst {
			fork, err := gitx.Run(wt, "merge-base", "origin/"+guard, "HEAD")
			if err != nil {
				continue // guard branch may not exist in this repo
			}
			countStr, err := gitx.Run(wt, "rev-list", "--count", "origin/"+pr.Base+".."+fork)
			if err != nil {
				return nil, err
			}
			count, _ := strconv.Atoi(countStr)
			if count > 0 {
				strays = append(strays, Stray{Repo: repo.Name, Branch: branch, Guard: guard, Fork: fork, Commits: count})
			}
		}
	}
	return strays, nil
}

// PR pushes each repo branch that has commits beyond the PR base and opens a
// pull request for it, after the stray guard passes.
func PR(ws *workspace.Workspace, name string, extraArgs []string) error {
	pr := ws.Config.PR

	strays, err := StrayCheck(ws, name)
	if err != nil {
		return err
	}
	if len(strays) > 0 {
		for _, s := range strays {
			fmt.Fprintf(os.Stderr,
				"%s: %s sits on %s, which is %d commit(s) ahead of %s.\nA PR into %s would carry those commits, which are not yours.\n\n  git rebase --onto origin/%s %s %s\n  re-run the suite — the base moved\n  git push --force-with-lease\n\nDo NOT plain-rebase onto %s: it replays %s's commits too and conflicts on files %s already has.\n",
				s.Repo, s.Branch, s.Guard, s.Commits, pr.Base, pr.Base,
				pr.Base, s.Fork[:12], s.Branch, pr.Base, s.Guard, pr.Base)
		}
		return fmt.Errorf("refused: branch on the wrong base")
	}

	opened := 0
	for _, repo := range ws.Config.Repos {
		wt := filepath.Join(ws.TreesPath(), name, repo.Name)
		if _, err := os.Stat(wt); err != nil {
			continue
		}
		branch, err := gitx.Run(wt, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return err
		}
		countStr, err := gitx.Run(wt, "rev-list", "--count", "origin/"+pr.Base+"..HEAD")
		if err != nil {
			return err
		}
		if countStr == "0" {
			fmt.Printf("%s: no commits beyond %s — skipping\n", repo.Name, pr.Base)
			continue
		}
		fmt.Printf("%s: pushing %s\n", repo.Name, branch)
		if err := gitx.RunInteractive(wt, "push", "-u", "origin", branch); err != nil {
			return err
		}
		args := append([]string{"pr", "create", "--base", pr.Base, "--head", branch}, extraArgs...)
		gh := exec.Command("gh", args...)
		gh.Dir = wt
		gh.Stdin = os.Stdin
		gh.Stdout = os.Stdout
		gh.Stderr = os.Stderr
		if err := gh.Run(); err != nil {
			return fmt.Errorf("gh pr create failed in %s", repo.Name)
		}
		opened++
	}
	if opened == 0 {
		return fmt.Errorf("no repo in tree %q has commits beyond %s", name, pr.Base)
	}
	return nil
}
