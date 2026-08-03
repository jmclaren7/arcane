# Fork changes

## Purpose of this fork

`jmclaren7/arcane` is a personal development fork of
[`getarcaneapp/arcane`](https://github.com/getarcaneapp/arcane), kept
deliberately thin and tracking upstream `main` closely. It exists to run
Arcane in a self-hosted lab from images the fork builds itself
(`ghcr.io/jmclaren7/arcane` and `ghcr.io/jmclaren7/arcane-agent`, tag `next`),
which upstream's release pipeline can't provide to a fork because it depends on
GoReleaser Pro, Depot runners, and cosign secrets. Everything the fork carries
falls into three buckets: **infrastructure adaptations** so CI, image builds,
and the dev container work outside upstream's environment (changes #3, #4, #6);
**documentation** describing the fork itself and filling gaps in the
contributor setup docs (#1, #5); and a small set of **behavioural fixes and
UI refinements** that are genuinely upstreamable but haven't been submitted or
merged yet (#2, #7–#11). The fork deliberately carries no product features of
its own and no divergent architecture — every rebase resolves conflicts in
favour of upstream unless the entry below marks the fork side as intentional,
and any change upstream implements independently is dropped rather than
maintained.

This file is the authoritative list of changes that set this fork
(`jmclaren7/arcane`, branch `main`) apart from upstream
(`getarcaneapp/arcane`, branch `main`). It is written to be used by a human
or an AI agent when **rebasing onto a newer upstream or re-applying these
changes**. Implementation details are not authoritative since they might conflict with incoming changes, intent and purpose are the critical aspect of this list.

When you rebase, work through every entry below. For each one:

1. Check whether upstream has since implemented an equivalent fix. If it has,
   **drop** the fork change and move it to "Superseded / now upstream", naming
   the upstream commit that replaced it.
2. Otherwise re-apply it, adapting to any code that moved. Re-run the listed
   verification.
3. **Build and type-check the result, even when the rebase reported no
   conflicts.** Git only detects conflicts between overlapping hunks; an
   upstream change elsewhere in a file the fork also edits (an import removed, a
   helper renamed) replays cleanly and breaks only at compile time. This has
   already happened once — see the `50c3d5d` note below. The minimum gate is
   `GOEXPERIMENT=jsonv2 go build ./...` and `go vet ./...` in `backend/`, plus
   `pnpm install --frozen-lockfile && pnpm -C frontend check`.
4. Keep this file in sync: update the "Last rebased onto" marker, and move
   entries between the "Active" and "Superseded" sections as upstream evolves.

> **Last rebased onto upstream:** `c9fa64b` — _fix: forward icon catalog
> setting over tunnel endpoints (#3495)_, on 2026-08-03. _(Previously
> `50c3d5d`, 2026-07-27.)_
>
> This rebase carried 64 new upstream commits, including several structural
> refactors: per-user passkey MFA / passwordless login (`46a0682`, which took
> migration numbers `069`–`070`), an automation-to-actors refactor (`3bdb4e6`),
> a gorilla→coder websocket migration (`2f1a16a`), a unified edge tunnel
> transport lifecycle with the proto relocated to `backend/proto` (`5ea6675`),
> a Go workspace (`go.work`, `081e297`), a backend test refactor (`de735b0`),
> plus fixes (self-upgrade image-label refresh, image event-watcher gating,
> serialized bulk deletes, lifecycle permission diagnostics), a gated admin
> password-reset CLI, dependency bumps, and Crowdin updates. Three things
> needed manual work:
>
> 1. **Change #11 relocated.** Upstream's `5ea6675` moved the register-send /
>    await-registration sequence out of `connectAndServeGRPC`
>    (`client_transport_grpc.go`) into the shared transport-agnostic
>    `serveTunnelSessionInternal` in `client.go`. The fork's io.EOF
>    fall-through was re-applied there — `client_transport_grpc.go` is now
>    entirely upstream's — and as a side effect the fix now covers every
>    transport, not just gRPC. The entry below reflects the new location.
> 2. **Migration renumbered `069` → `071`.** Upstream's passkey work claimed
>    `069`/`070`, colliding with the fork's
>    `069_add_gitops_sync_inject_commit_env.sql` (change #12); Goose refuses
>    duplicate version numbers. Renamed in both `sqlite/` and `postgres/`.
>    **Deployment caveat:** an instance that already applied the fork's old
>    `069` records version 69 as applied, so upstream's registry-names `069`
>    would be skipped and the renamed `071` would fail re-adding the existing
>    column — fix the `goose_db_version` table by hand (rename the fork's 69
>    entry to 71) before starting the upgraded image, or start from a backup.
> 3. **Change #12's `env.go` hunk conflicted** because upstream reordered
>    `BuildOverrideEnvContent` after `BuildAdditiveOverrideEnvContent`; the
>    naive replay would have duplicated `BuildOverrideEnvContent`. Resolved by
>    inserting only the fork's new helpers and keeping upstream's copy in its
>    new position.
>
> The actors and test refactors did not move change #12's callsites
> (`prepareSyncSource` / `stageDirectorySyncInternal` still funnel through
> `gitMetadataEnvContentInternal`), and the go-git `5.19.2` bump left change
> #8 untouched. The fork `ci.yml` inherited upstream's loosened action pins
> (`golangci-lint-action@v9`, `actions/cache@v6`) via clean replay;
> `build-next-images.yml`'s `docker/login-action` pin was synced to `v4.5.2`
> to match upstream's `release.yml` bump. Verified post-rebase:
> `GOEXPERIMENT=jsonv2 go build ./...` and `go vet ./...` clean over the whole
> backend, `go test ./pkg/gitutil/... ./pkg/libarcane/edge/...
> ./pkg/projects/... ./internal/database/...` and the GitOps service tests
> passing, and `pnpm -C frontend check` reporting 0 errors / 0 warnings. None
> of the 64 commits supersede an active fork change — all 12 remain necessary
> (redundancy checks re-verified against `c9fa64b`).
>
> Earlier rebase (`50c3d5d`, 2026-07-27): carried 22 upstream commits — a
> four-commit static-analysis and error-handling sweep, credential-target and
> browse-path hardening, an updates-page redesign, several backend fixes,
> CLI/API contract alignment, dependency bumps, and Crowdin updates. All 17
> fork commits replayed with zero textual conflicts, but one **semantic**
> conflict produced a tree that did not compile: upstream removed the unused
> `fmt` import from `backend/pkg/gitutil/git.go` while fork change #8's
> replayed hunk still called `fmt.Errorf` further down the file. Resolved in
> favour of upstream (`errors.Errorf` from `emperror.dev/errors`). **Lesson
> for future rebases: a conflict-free rebase is not a verified rebase — always
> build and type-check afterwards.**
>
> Earlier rebase (`a3a56d6`, 2026-07-25): carried 7 upstream commits —
> raw-CLI-output/watch-mode for Docker operations, a dashboard redesign
> (clickable tiles, volumes tile, configurable default landing page), an
> environment-settings page/tabs redesign, appearance settings moving to the
> user profile view, a Swarm resource-scoping/stack-deploy fix, and an updater
> dependency bump. Zero conflicts. Upstream's dashboard landing-page work
> rewrote `frontend/src/lib/utils/navigation.ts` and trimmed an icon import in
> the projects `+page.svelte`; both files also carry fork change #9
> (`shortenGitCommit`), which sits in an unrelated part of each file and merged
> clean.
>
> Earlier rebase: crossed several large upstream refactors — echo v4→v5,
> wire→fx DI, samber/mo + samber/hot adoption, unified error handling — plus a
> frontend `$lib` → `#lib` import-alias migration and a workflows reshuffle.
> None of the backend refactors touched the fork's surface (`gitutil.Clone` /
> `TestConnection` merged clean); the frontend changes all needed the `#lib`
> alias adopted while re-applying.

---

## Active changes

### 1. "About this fork" README banner

- **Files:** `README.md`
- **What:** Prepend a blockquoted "About this fork" section above upstream's
  README that explains this is a dev fork, points at `FORK.md`, and
  lists the published image names/tags.
- **Why:** Makes the fork's purpose and image locations obvious to anyone who
  lands on the repo, and links to the change list.
- **Re-apply notes:** Insert the block immediately before upstream's first
  README line (`<div align="center">`). Keep the one-sentence summary of
  carried changes in sync with the Active list below (drop mentions of any
  change that moves to "Dropped").

### 2. Set background early to prevent white flash

- **Files:** `frontend/src/app.html`
- **What:** Inject an inline `<style>` (light/dark `html` background +
  `color-scheme`) and a small bootstrap `<script>` that reads the persisted
  `mode-watcher-mode` (default `system`), toggles the `dark` class on `<html>`
  and sets `colorScheme` before hydration.
- **Why:** Eliminates the flash of incorrect theme on first paint.
- **Re-apply notes:** Insert the block right after the two `theme-color`
  `<meta>` tags, before `%sveltekit.head%`. The oklch values must match
  upstream's `theme-color` metas (currently `oklch(1 0 0)` light /
  `oklch(0.141 0.005 285.823)` dark). Depends on `mode-watcher` and its
  `mode-watcher-mode` localStorage key — confirm both still exist
  (`mode-watcher` in `frontend/package.json`).
- **Redundancy check:** Upstream `app.html` still has no early-background
  handling (only the two `theme-color` metas) — **keep**.

### 3. Preinstall Bun in the dev Dockerfile

- **Files:** `docker/Dockerfile.dev`
- **What:** In the `frontend-dev` stage, `apt-get install ca-certificates curl
  unzip`, install Bun, and symlink it to `/usr/local/bin/bun`.
- **Why:** `pnpm check` runs `svelte-check-rs`, which shells out to Bun and
  tries to auto-install it; the slim Node image lacks curl/unzip so the
  auto-install fails.
- **Re-apply notes:** Add the `RUN` block right after the
  `FROM ... AS frontend-dev` line. Keep the Node base image tag in sync with
  upstream (currently `node:26-trixie-slim`).
- **Redundancy check:** `svelte-check-rs` is still the `check` script in
  `frontend/package.json` and upstream's `frontend-dev` stage still omits Bun
  (the `ca-certificates`/`curl` install lives in the separate `backend-dev`
  stage, not the frontend one) — **keep**.

### 4. Exclude nested build artifacts from the Docker build context

- **Files:** `.dockerignore`
- **What:** Add recursive `**/node_modules`, `**/build`, `**/.svelte-kit`
  alongside the existing top-level entries.
- **Why:** Nested workspace folders were copied into the build context and
  caused build failures (e.g. on Windows, where the bundled modules differed).
- **Redundancy check:** Upstream added some recursive media/test patterns
  (`**/*.test.js`, `**/.DS_Store`, `**/*.mp4`), but the build-artifact entries
  `node_modules`, `build`, and `.svelte-kit` are still top-level only —
  **keep**.

### 5. Update contributor dev docs

- **Files:** `CONTRIBUTING.md`
- **What:** Split Prerequisites into Required (Docker) and Optional (host
  tools for the Justfile shortcuts: `just`, `pnpm`/Node, Go 1.26+,
  `golangci-lint`, with a Windows `winget` one-liner); clarify that Justfile
  recipes run on the host, not in the dev containers; and expand "Manual
  Commands" into three options (Justfile, `dev.sh shell`, and one-shot
  `docker compose exec` with `-p arcane-dev` + `pnpm -C frontend`).
- **Why:** Upstream's Prerequisites say only "Docker & Docker Compose (that's
  it! 🎉)", so contributors who use the Justfile shortcuts or run checks
  manually hit missing host tools or the wrong Compose project name.
- **Re-apply notes:** The patch anchors on the `### Prerequisites`,
  `### Justfile Shortcuts`, and `### Manual Commands` headings. Confirm the dev
  script path (`./scripts/development/dev.sh`) and the Compose project name
  (`arcane-dev`) still match before re-applying.
- **Redundancy check:** Upstream `CONTRIBUTING.md` Prerequisites still read
  "Docker & Docker Compose (that's it! 🎉)" with no optional-tools guidance —
  **keep**.

### 6. CI/workflows adapted for this fork

- **Files:** `.github/workflows/ci.yml`, `.github/workflows/build-next-images.yml`
- **Intent:** Make CI run on a fork without upstream-only
  infrastructure:
  - Run on the `main` branch (CI on push/PR; next images on push to
    `main`).
  - Use standard `ubuntu` runners instead of `depot-*` runners; use
    `docker/setup-qemu-action` + `docker/setup-buildx-action` instead of the
    depot buildx driver.
  - Drop steps that need secrets/licenses unavailable on forks: GoReleaser Pro
    release, cosign signing, depot test reporting.
  - Drop the `deadcode` and `cli-e2e-tests` jobs (gated on the `getarcaneapp`
    owner / depot secrets).
  - Build & push with `docker/build-push-action` to
    `ghcr.io/<owner>/arcane` and `ghcr.io/<owner>/arcane-agent` (tag `next`),
    dropping the upstream `manager`/`arcane-headless`/`agent` image
    names/aliases and the `linux/arm/v7` + `linux/riscv64` targets (fork
    builds `linux/amd64,linux/arm64` only).
- **Re-apply notes:** This is the highest-churn area on rebase — upstream
  frequently rewrites these workflows. Re-derive from upstream's **new**
  workflow files and re-apply the transformations above, rather than
  force-keeping the fork's stale copies, so upstream CI improvements are picked
  up. (For `ci.yml`, a 3-way cherry-pick of the fork's CI commit onto the new
  upstream `ci.yml` cleanly carries the fork's removals while inheriting
  upstream's pin bumps; `build-next-images.yml` is a wholesale fork rewrite, so
  re-derive it by hand and only sync upstream's action pins into it.) Keep
  action/toolchain pins in sync with upstream. At the `73d13dc` rebase the fork
  `ci.yml` adopted upstream's current pins (node 26, `actions/checkout@v7`,
  `actions/setup-go@v7`, `golangci-lint-action@v9.3.0`, `actions/cache@v6.1.0`),
  inherited upstream's new `Lint protobuf definitions` step and the renamed
  `type-check` job (`pnpm install --frozen-lockfile` + `just lint js`), and kept
  the fork's `push`/`main` triggers, `contents: read` permissions, the
  `github.ref` concurrency fallback (needed for push events), `ubuntu-latest`
  runners, and `docker/setup-buildx-action` in `e2e-tests`. At the `c9fa64b`
  rebase the fork `ci.yml` inherited upstream's loosened pins
  (`golangci-lint-action@v9`, `actions/cache@v6`) via clean replay.
  `build-next-images.yml` pins `actions/checkout@v7.0.0` and
  `docker/login-action@v4.5.2` (synced to upstream's `release.yml` bump).
  The fork intentionally keeps the agent image named `arcane-agent` (not
  upstream's `arcane-headless`) because it reads better when the repo owner is
  not "Arcane"; preserve published image names so existing pullers don't break.
- **Redundancy check:** At `73d13dc` upstream **removed** its own
  `build-next-images.yml` (and `merge-conflict.yml`) and now publishes images
  only from `release.yml` via GoReleaser Pro, which a fork can't run. So the
  fork's `build-next-images.yml` no longer has an upstream counterpart to
  re-derive from — it is a standalone fork workflow (a **modify/delete** conflict
  on rebase; resolve by keeping the fork file). Verify the Dockerfile paths it
  references still exist (`docker/Dockerfile`, `docker/Dockerfile-agent` — both
  present, both still take `VERSION`/`REVISION` build-args). Upstream's `ci.yml`
  still uses `depot-*` runners, Depot CLI, and the `deadcode` + `cli-e2e-tests`
  jobs. The fork adaptation is still required. **keep.**
- **Out of scope:** `build-pr-images.yml` and `release.yml` are left at
  upstream — the fork has never customised them.

### 7. GitOps manual sync: honest feedback and per-row spinner

- **Files:** `frontend/src/routes/(app)/environments/[id]/gitops/sync-table.svelte`
- **What:** "Sync Now" now reads `result.success` from the response body rather
  than treating any 2xx as a completed sync — an overlapping run is coalesced
  server-side and returns `success=false`. The UI shows `toast.success` (with
  the server's message as a description) only when the sync actually applied and
  `toast.warning` with the server message otherwise. Loading state is tracked
  per row as a set of running ids (`syncingIds`); each running row shows a
  spinner + "Syncing…" in its status column and disables only its own action, so
  independent syncs run concurrently.
- **Why:** Upstream fires a blanket `toast.success` on any 2xx and gates a
  single table-wide `isLoading.syncing` flag, so a coalesced/no-op sync falsely
  reports success and there is no visible per-row progress.
- **Re-apply notes:** Anchors on `handlePerformSync`, the `isLoading` /
  `syncingIds` state, the `StatusCell` snippet, and the `RowActions`
  `DropdownMenu.Item`. The `onSuccess(data)` callback depends on
  `handleApiResultWithCallbacks` passing the parsed response body. Depends on
  message keys `common_syncing`, `git_sync_success`, `git_sync_failed`
  (`frontend/messages/en.json`). Keep in step with change #8's backend
  coalescing that returns `success=false` for an overlapping run. The spinner
  belongs in the status column, **not** in the `DropdownMenu.Item` (the menu
  closes on select, so a spinner there is never seen).
- **Redundancy check:** Upstream `sync-table.svelte` still toasts success on any
  2xx and uses the single `isLoading.syncing` flag with no status-column spinner
  — **keep**.

### 8. Shallow, tag-less GitOps clones and ls-remote connection test

- **Files:** `backend/pkg/gitutil/git.go`
- **What:** `Client.Clone` sets `Depth: 1` and `Tags: git.NoTags`, so GitOps
  fetches only the working tree at the branch tip instead of full history and
  tags. `Client.TestConnection` proves reachability and credentials with an
  in-memory `listRemoteReferences` (ls-remote) instead of a full
  clone-and-delete, still verifying the branch exists among the remote refs when
  a branch is set.
- **Why:** Full-history clones were the dominant cost of every sync (and of each
  browse / build-context clone) and grew with repo age; "Test Connection" cloned
  the entire repository only to delete it.
- **Re-apply notes:** `Depth`/`Tags` are added to the `cloneOptions` literal in
  `Clone`; `TestConnection` is rewritten to call `listRemoteReferences` and match
  `plumbing.NewBranchReferenceName(branch)`, returning a `branch %q not found`
  error otherwise. Confirm `listRemoteReferences` still exists in `git.go` before
  re-applying. Build the `gitutil` package after re-applying: the not-found error
  uses `errors.Errorf` (`emperror.dev/errors`, the idiom used throughout this
  file) and **must not** use `fmt.Errorf` — upstream removed the `fmt` import
  from `git.go` at `5a612eb`, and because that import sits far from
  `TestConnection`, a stale `fmt.Errorf` here replays without conflict and only
  fails at compile time. Caveat: a shallow, tag-less clone means any caller that
  relies on commit history or tags being present in the clone would break — none
  currently do.
- **Redundancy check:** Upstream `Clone` still does a full clone (no `Depth` /
  `Tags`) and `TestConnection` still clones-and-deletes — **keep**.

### 9. Short commit hash display with full hash on hover

- **Files:** `frontend/src/lib/utils/navigation.ts`,
  `frontend/src/routes/(app)/environments/[id]/gitops/sync-table.svelte`,
  `frontend/src/routes/(app)/projects/[projectId]/+page.svelte`
- **What:** Add a `shortenGitCommit` helper (and `SHORT_GIT_COMMIT_LENGTH = 7`)
  beside `toGitCommitUrl`. Everywhere a GitOps commit hash is shown — the sync
  table `CommitCell`, the project header, and the read-only git-managed alert —
  render the abbreviated hash with the full hash in a `title` tooltip. The commit
  link's `href` still uses the full hash so it resolves.
- **Why:** Full 40-character hashes are noisy in the UI; the short form reads
  better while the full value stays available on hover and in the link.
- **Re-apply notes:** The helper is appended to `navigation.ts` after
  `toGitCommitUrl`. Each display site derives `{@const shortCommit = ...}`,
  renders `shortCommit`, and adds `title={fullCommit}` (the sync table) or
  `title={project.lastSyncCommit}` (the project page). Keep every commit-link
  `href` on the full hash. Shares `sync-table.svelte` with change #7 — apply
  both when re-doing that file.
- **Redundancy check:** Upstream renders the raw full hash with no short form or
  `title` — **keep**.

### 10. Hide the copy button when the Clipboard API is unavailable

- **Files:** `frontend/src/lib/components/ui/copy-button/copy-button.svelte`
- **What:** Replace the `isSecure` gate (which rendered a disabled button with an
  "HTTPS required" tooltip) with a `canCopy` check
  (`typeof navigator !== 'undefined' && !!navigator.clipboard`) that hides the
  button entirely when the Clipboard API is unavailable — typically an insecure /
  non-HTTPS context. Removes the `{:else}` tooltip branch and its `Tooltip`
  import.
- **Why:** The Clipboard API is only exposed in secure contexts, so on insecure
  connections copying silently fails; offering a disabled button is confusing —
  better not to present an action that cannot work.
- **Re-apply notes:** `onMount` sets `canCopy`; the `{#if canCopy}` wraps the
  single `ArcaneButton` and the disabled-button + `Tooltip` fallback is deleted.
  The `common_copy_https_required` message key is now unused by this component
  but is **still shipped upstream** (`frontend/messages/en.json`) — leave the key
  in place, don't prune it.
- **Redundancy check:** Upstream `copy-button.svelte` still renders the
  disabled-button-with-tooltip fallback keyed on `isSecure` — **keep**.

### 11. Surface real rejection reason on tunnel register-send race

- **Files:** `backend/pkg/libarcane/edge/client.go`
- **What:** In `serveTunnelSessionInternal` (the shared register/heartbeat/
  message lifecycle for every transport), when the tunnel connection's `Send`
  of the register message fails with `io.EOF`, fall through to
  `awaitRegistrationInternal` instead of returning immediately, so the real
  terminal status (e.g. "invalid agent token") surfaces instead of the bare
  `"failed to send grpc tunnel register message: EOF"`.
- **Why:** Per gRPC semantics, a stream's terminal status only surfaces on
  `Recv`, not `Send` — when the manager rejects the stream before the
  client's `Send` completes, `SendMsg` returns a bare `io.EOF` and the actual
  rejection reason was lost. This also fixed the flaky
  `TestTunnelClient_connectAndServeGRPC_RegistrationRejected` test, which
  failed whenever the server's teardown won the race against `Send`.
- **Re-apply notes:** Originally applied in `connectAndServeGRPC`
  (`client_transport_grpc.go`); upstream's `5ea6675` transport-lifecycle
  unification moved the register/await sequence into
  `serveTunnelSessionInternal` in `client.go`, and the fix moved with it at
  the `c9fa64b` rebase (it now covers the websocket transport too — a
  fall-through on a non-EOF-signalling transport is harmless because `Recv`
  reports the close reason). Anchors on the
  `conn.Send(c.registerMessageInternal())` error check; wrap it in
  `if !errors.Is(err, io.EOF) { return ... }` and let `io.EOF` fall through.
  Requires the `io` import (already present in `client.go`).
- **Redundancy check:** Upstream's `serveTunnelSessionInternal` still returns
  immediately on any `Send` error, including `io.EOF` — **keep**.

### 12. GitOps commit-hash injection into the synced project's env

- **Files:** `backend/pkg/projects/env.go`, `backend/pkg/projects/env_test.go`,
  `backend/internal/models/gitops_sync.go`,
  `backend/internal/services/gitops_sync_service.go`,
  `backend/internal/services/gitops_sync_service_test.go`,
  `backend/internal/services/gitops_sync_service_unix_test.go`,
  `backend/resources/migrations/{sqlite,postgres}/071_add_gitops_sync_inject_commit_env.sql`,
  `types/gitops/gitops.go`, `frontend/src/lib/types/automation.ts`,
  `frontend/src/lib/components/dialogs/gitops-sync-dialog.svelte`,
  `frontend/messages/en.json`
- **What:** Add an opt-in `injectCommitEnv` flag to a GitOps sync. When it is on,
  the sync appends `ARCANE_GIT_COMMIT`, `ARCANE_GIT_COMMIT_SHORT` and
  `ARCANE_GIT_BRANCH` to the Git-sourced env content it hands to the existing
  three-file env merge (`.env.git` + `project.env` → `.env`), so compose can
  interpolate them and the `autoInjectEnv` setting can hand them to every
  service. `projects.BuildGitMetadataEnvContent` builds the block; the override
  builders (`BuildOverrideEnvContent`, `BuildAdditiveOverrideEnvContent`) skip
  the managed keys so a stale copy read back out of `.env` can never be pinned
  into the user's override; `envContentChangedInternal` ignores a *change in
  their values* so a commit that leaves the synced files untouched does not
  redeploy a running project, while gaining or losing the keys (the toggle being
  switched on or off) still counts as a change so running containers pick the
  new environment up.
  Covers single-file, directory, and swarm-stack syncs — all three already
  funnel their Git env through one place.
- **Why:** The synced commit was already resolved and persisted
  (`gitops_syncs.last_sync_commit`) and shown in the UI, but nothing carried it
  into the container, so a deployed application could not report the commit it
  was built and deployed from.
- **Re-apply notes:** The migration is Goose-versioned — on every rebase check
  whether upstream has claimed the fork migration's number and, if so, rename
  it (both dialects) to the next free number (it moved `069` → `071` at the
  `c9fa64b` rebase when upstream's passkey work took `069`/`070`; see the
  deployment caveat in the rebase notes above). Injection has exactly two
  callsites, both feeding
  `gitMetadataEnvContentInternal`: `prepareSyncSource` (covers single-file and
  swarm, which both read `source.envContent`) and `stageDirectorySyncInternal`
  (directory sync, whose env comes from `partitionReservedRootEnvFilesInternal`).
  The directory path needs the commit hash threaded through
  `syncProjectDirectoryInternal` → `stageDirectorySyncInternal`. Metadata is
  appended *after* the repo's own env so dotenv's last-assignment-wins rule keeps
  a repo-supplied key from spoofing it. Known corner: disabling the flag drops
  the keys on the next sync unless the repository ships no `.env` of its own —
  there nothing replaces the Git-sourced env, so the last injected values stay in
  the project's `.env` until removed by hand.
- **Redundancy check:** Upstream has no commit-injection option; its GitOps sync
  writes only the repository's own env content — **keep**.

---

## Superseded / now upstream

Changes the fork used to carry that upstream has since implemented
independently. Each entry names the upstream change that replaced it. Do
**not** re-introduce them:

- **Sanitize discovered project names** *(was Active #1:
  `project_service.go`, `project_service_test.go`)* — **superseded by upstream
  `0feb1007`** _(refactor: streamline project service with better reusable
  functions (#3157))_, which reworked
  `upsertProjectForDir`. New projects are now created with
  `Name: composeMetadata.resolvedProjectName`, where `resolvedProjectName`
  defaults to `projects.NormalizeProjectName(dirName)` (compose-go
  normalization → `[a-z0-9_-]`, a subset of the editor-accepted
  `[A-Za-z0-9_-]`). Existing rows self-heal on sync in the same function via
  `NormalizeProjectName(existing.Name) != existing.Name`. This covers both
  halves of the fork change (a valid `Name` on discovery **and** self-healing
  existing invalid names), so the fork's `SanitizeProjectName` patch and its
  tests are redundant. At the 2.0.x rebase upstream still seeded
  `Name: dirName` unsanitised, which is why this was Active then.
  _Dropped at the 2026-07-07 rebase onto `b501c49`._
- **Keep LF line endings for scripts** *(was Active #5: `.gitattributes`)* —
  **superseded by upstream `fb0cb2e5`** _(feat: add pre-deploy hook for GitOps
  project syncs (#3022))_, which added upstream's own
  `.gitattributes` forcing `eol=lf` for `*.sh`
  (plus `.husky/*`, `.husky/_/*`, `Justfile`), which covers the real concern
  (bash shebangs broken by Windows `autocrlf`). The fork's `*.bash` rule
  matched no files in the tree. The only rule upstream lacks is
  `Dockerfile text eol=lf`; modern BuildKit tolerates CRLF in Dockerfiles, so
  this residual is marginal and was not re-added. If it's ever wanted, append
  that single line to upstream's `.gitattributes` rather than re-adding the
  fork's own file (which would conflict). _Dropped at the 2026-07-07 rebase
  onto `b501c49`._
- **User avatar 404 when no email is set** *(was: `sidebar-user.svelte`)* —
  **superseded by upstream `51977fc0`**, which extracted `sidebar-user.svelte`
  into its own component with an `if (!email) return ''` guard at the top of
  `getGravatarUrl`, so the Gravatar 404 no longer occurs and the fork's
  `&& user?.email` call-site guard is redundant.
  _Dropped at the 2.0.x rebase (2026-06-13)._
- **Toast on project form validation failure** *(was part of the project-name
  fix, `projects/[projectId]/+page.svelte`)* — **superseded by upstream
  `8c750764`** _(fix: project save button not showing in tree view (#2163))_,
  whose `handleSaveChanges`
  now shows `toast.error(m.templates_validation_error())` via its own
  `hasAnyErrors` guard before the silent `if (!validated) return`, so the extra
  toast would be duplicative. _Dropped at the 2.0.x rebase (2026-06-13)._
