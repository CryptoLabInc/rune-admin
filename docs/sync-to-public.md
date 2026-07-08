# Public Repository Sync

This repository (`geuna0204/rune-admin-private`) is the **private development
home** of Rune-Vault. The public repository
[`CryptoLabInc/rune-admin`](https://github.com/CryptoLabInc/rune-admin) exists
so that customers can audit the exact source code they run and verify it
contains no malicious code.

## How the sync works

The workflow [`.github/workflows/sync-to-public.yml`](../.github/workflows/sync-to-public.yml)
runs on every push to `main` in this repository and publishes the current
state of `main` to the public repository:

1. It fetches the public repository's `main` head.
2. It creates **one commit** whose tree is the entire file tree of private
   `main` and whose parent is the public `main` head (`git commit-tree`).
3. It pushes that commit to public `main`.

Because only the resulting file tree is published, the public repository
receives a single squashed commit per sync regardless of how many commits,
merges, or pull requests happened privately. Internal commit messages, author
names/emails, and intermediate changes are never transferred. If the trees
are identical (nothing changed), the sync exits without pushing.

Authentication uses a write deploy key on the public repository; the private
key is stored as the `PUBLIC_REPO_DEPLOY_KEY` Actions secret in this
repository. The workflow can also be run manually via *workflow_dispatch*.

## Branching model

```
feature/xxx ──PR──▶ develop ──merge──▶ main ──(automatic)──▶ public main
                    (default,           (release/publish      one squashed
                     integration)        trigger)              commit
```

- `develop` — the default branch. All feature branches are merged here via
  pull request. CI runs here; this is where integrated work is tested.
- `main` — the **publish trigger**. Merging `develop` into `main` and pushing
  is a release action: it immediately publishes the resulting file tree to
  the public repository. Only merge into `main` when the integrated state on
  `develop` has been tested and is ready to be public.

The granularity of public commits is therefore controlled by how often
`develop` is merged into `main`: merge once per release and the public
repository shows one commit per release.

## Rules

- **Never push directly to public `CryptoLabInc/rune-admin`.** Any commit
  made there directly will be overwritten by the next sync.
- **Never push directly to `main` here.** Treat `main` as release-only;
  promote tested state from `develop`. (Branch protection is not available
  on this plan, so this is enforced by convention.)
- **Everything committed to `main` becomes public.** Never commit secrets,
  internal URLs, customer names, or non-public documents anywhere in the
  tree — even temporarily, since the full tree of `main` is published.
- Git tags are not synced. Public release tags, if desired, must be created
  on the public repository separately.

## Operational notes

- The first squashed sync commit on the public repository is
  `964673a` (parent `d3653ef`, the last commit of the original public
  history, which was intentionally preserved).
- To rotate credentials: delete the deploy key on the public repository,
  generate a new ed25519 key pair, register the public key as a write deploy
  key on `CryptoLabInc/rune-admin`, and update the `PUBLIC_REPO_DEPLOY_KEY`
  secret here.
- If a sync run fails, fix the cause and re-run it from the Actions tab
  (*Sync to public repository* → *Run workflow*).
