# vtree

Multi-repo git-worktree workspaces with per-tree MySQL schemas.

vtree manages a workspace of **trees**: one directory per feature, holding a git worktree for
every configured repo, with its own allocated ports and its own MySQL schemas (`<prefix><tree>`
and `<prefix><tree>_test`). Local dev runs on the same engine production runs on — there is no
SQLite mode.

It is deliberately conservative about destruction:

- **There is no `prune` and no bulk removal.** Trees are removed by name, one at a time.
  No heuristic can reliably tell a finished tree from one that is merged *and* still being
  worked in — we have the data-loss incident to prove it.
- **`down` refuses by default** when a tree holds uncommitted changes (untracked files count)
  or commits that exist on no remote. Removing a worktree deletes its branch, so unpushed
  commits die with it. `--force` must be typed. If vtree cannot *verify* a repo's state, that
  is also a refusal — errors fail closed.

## Layout

```
workspace/
├── .vtree/
│   ├── vtree.yaml     # workspace definition — committed, shared
│   ├── local.yaml     # this machine only (DB credentials) — gitignored
│   ├── scripts/       # setup + custom commands
│   └── templates/     # files copied into each new tree
├── repos/             # source clones (one per configured repo)
└── trees/<name>/      # one worktree per repo, plus a .vtree-tree.json manifest
```

## Commands

| Command | What it does |
| --- | --- |
| `vtree init` | Scaffold a workspace interactively |
| `vtree install` | Clone the configured repos |
| `vtree up <name>` | Create a tree: branches, ports, schemas, env files, setup |
| `vtree down <name>` | Remove a tree; refuses if work would be lost |
| `vtree ls` / `status --json` | Trees with ports, branches, dirty/unpushed state |
| `vtree run <cmd> [tree]` | Run a workspace-defined command |
| `vtree pr <tree>` | Push and open PRs, with a wrong-base drift guard |
| `vtree adopt <tree>` | Bring a pre-vtree tree under management |
| `vtree doctor` | Check prerequisites and workspace health |

## Install

Grab the archive for your machine from the [latest release](https://github.com/Quentin-JH/vtree/releases),
then put the binary on your PATH:

```
tar -xzf vtree_*.tar.gz && mv vtree ~/.local/bin/
```

(Or, with Go installed: `go install github.com/Quentin-JH/vtree@latest`.)

## Joining an existing workspace

```
git clone <workspace-repo> && cd <workspace>
vtree init      # prompts for YOUR machine's MySQL settings → .vtree/local.yaml
vtree install   # clones the configured repos
vtree doctor    # verifies everything is reachable
vtree up my-feature
```

The workspace's shared definition (`.vtree/vtree.yaml`, scripts, templates) comes with the clone;
`local.yaml` is the only per-machine piece, and vtree refuses database operations until it exists —
there are no baked-in connection defaults to silently hit the wrong server.

## Starting a new workspace

Run `vtree init` in an empty directory — it walks you through repos, ports, database, and PR
settings, writes `.vtree/vtree.yaml` and `.gitignore`, and validates its own output against the
strict config loader.

## Development

```
go test ./...
go build -ldflags "-X github.com/Quentin-JH/vtree/cmd.Version=$(git describe --tags --always)"
```
