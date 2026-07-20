# Upgrading Rune Console

Rune Console updates replace only the service binary. Before replacement, the
updater stops the daemon and creates a private snapshot containing the complete
configured storage directory (including both SQLite databases and any WAL/SHM
files), `runeconsole.conf`, the key directory, and the configured TLS files.
The default snapshot root is `/var/backups/runeconsole`.

Official release binaries support Linux amd64/arm64 with systemd and glibc
2.38 or newer, plus Apple Silicon macOS 14 or newer. Intel macOS is not
supported because `runespace-sdk v1.0.0` does not contain a darwin/amd64
library slice. OpenSSL and the Linux C++ runtime are statically linked into
release binaries; Linux glibc and the macOS system libraries remain platform
dependencies.

## Console update prompt

For installations created by the official installer, the authenticated console
owner sees a non-blocking card in the upper-right corner only when GitHub's
latest stable release is newer than the running version. Selecting **Update**
queues that exact release for a separately supervised root helper; the web
daemon itself never runs with elevated privileges or stops itself.

The helper verifies that the queued tag is still the latest stable release,
then uses the same checksum, candidate-version, backup, health-check, and
rollback flow as the CLI below. The card remains visible while the daemon
restarts and reloads the embedded web console after the new version becomes
healthy. Choosing **Later** hides only that release for the current browser
session.

The installer provisions `runeconsole-update.path` and
`runeconsole-update.service` on Linux, or
`com.cryptolabinc.runeconsole-updater` on macOS. The privilege boundary uses
only fixed request/status paths below `/var/lib/runeconsole-updater`; those
files are separate from customer databases, configuration, keys, TLS files,
and backups. If the helper is absent, the GitHub check fails, the platform is
unsupported, the durable-state paths escape the official installation root,
or the server is air-gapped, no card is shown. Custom storage layouts can
still use the connected or offline CLI flows below, which are not constrained
by the service helper's filesystem sandbox.

## Connected server

Verify the latest published GitHub Release without stopping the daemon:

```sh
runeconsole update --check
```

Install it:

```sh
sudo runeconsole update
```

To select a release explicitly:

```sh
sudo runeconsole update --version v1.1.0
```

The command verifies the release archive against `SHA256SUMS`, checks the
candidate binary's reported version, creates the state snapshot, atomically
replaces the installed binary, and starts the service. If startup or the health
check fails, it restores the previous binary and pre-update state before
restarting the old version. Downgrades and forced state replacement are not
supported.

Successful updates print the exact snapshot directory. Snapshots are not
deleted automatically.

The official installer places a `.runeconsole-managed` ownership marker in the
configured storage and key roots. A manual installation must use dedicated
directories and place a file with the exact content `runeconsole-managed-v1`
(followed by a newline) in each root; the updater refuses recursive backup or
rollback without these markers.

## Air-gapped server

On a connected machine, download `SHA256SUMS` and the archive matching the
server from the same GitHub Release. For example:

```text
runeconsole_v1.1.0_linux_amd64.tar.gz
SHA256SUMS
```

Transfer both files into the internal network, then run the same verification
and update flow with local inputs:

```sh
runeconsole update --check \
  --version v1.1.0 \
  --archive /media/runeconsole_v1.1.0_linux_amd64.tar.gz \
  --checksums /media/SHA256SUMS

sudo runeconsole update \
  --version v1.1.0 \
  --archive /media/runeconsole_v1.1.0_linux_amd64.tar.gz \
  --checksums /media/SHA256SUMS
```

## Preparing a release

The first production tag must point to the commit containing the updater and
the database migration runners, not directly to baseline commit `f4ad3db`.
That baseline defines schema version 1; the migration runners adopt it without
changing existing rows.

Before pushing the first tag, enable GitHub immutable releases for the
repository and protect `v*` tags from update or deletion. The workflow verifies
that the remote tag, checked-out commit, and workflow commit are identical
immediately before publication.

Push a canonical `vMAJOR.MINOR.PATCH` tag. The tag workflow builds three
platform archives (`linux/amd64`, `linux/arm64`, and `darwin/arm64`) plus
`SHA256SUMS`, then creates the GitHub Release only after all artifacts are
ready. Do not create or publish the Release manually, and do not announce it
until the workflow has passed. Manual workflow runs are build-only dry runs.

The repository currently contains older public internal-test releases. Treat
`v1.0.0` as the first production release operationally; its release note is
created explicitly instead of generating notes from those internal tags.

For later releases, never edit a shipped schema migration. Append the next
ordered migration and increment the corresponding schema version instead.
