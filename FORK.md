# Fork changes

## Purpose of this fork

`jmclaren7/arcane` is a personal development fork of
[`getarcaneapp/arcane`](https://github.com/getarcaneapp/arcane), kept
deliberately thin and tracking upstream `main` closely. It exists to run Arcane
in a self-hosted lab from images the fork builds itself
(`ghcr.io/jmclaren7/arcane` and `ghcr.io/jmclaren7/arcane-agent`, tag `next`),
which upstream's release pipeline can't provide to a fork because it depends on
GoReleaser Pro, Depot runners and cosign secrets. Everything the fork carries
falls into three buckets: **infrastructure adaptations** so CI and image builds
work outside upstream's environment (changes #3, #9); **documentation**
describing the fork itself and filling gaps in the contributor setup docs (#1,
#2); and a small set of **behavioural fixes, UI refinements and self-contained
features** that are genuinely upstreamable but haven't been submitted or merged
yet (#4–#7, #10–#12) — plus one piece of fork-only debt, the migration
bookkeeping repair (#8). The fork deliberately carries no divergent
architecture and nothing that couldn't be offered upstream as-is: every rebase
resolves conflicts in favour of upstream unless the entry below marks the fork
side as intentional, and any change upstream implements independently is
dropped rather than maintained.

This file is the authoritative list of changes that set this fork
(`jmclaren7/arcane`, branch `main`) apart from upstream
(`getarcaneapp/arcane`, branch `main`). It is written to be used by a human or
an AI agent when **rebasing onto a newer upstream or re-applying these
changes**. Implementation details are not authoritative, since they conflict
with incoming changes; intent and purpose are the critical part of this list.

> **Last rebased onto upstream:** `b8c837ddf` — _chore(translations): update
> translations via Crowdin (#4667)_, on 2026-11-07.
> _(Previously `919bd842e`, 2026-10-24.)_

## How to rebase

Work through every entry below. For each one:

1. Check whether upstream has since implemented an equivalent fix. If it has,
   **drop** the fork change and move it to "Superseded / now upstream", naming
   the upstream change that replaced it.
2. Otherwise re-apply it, adapting to any code that moved. Re-derive from
   upstream's **new** files rather than force-keeping the fork's stale copies,
   so upstream's own improvements are picked up.
3. **Build and type-check the result, even when the replay reports no
   conflicts.** Git only detects conflicts between overlapping hunks; an
   upstream change elsewhere in a file the fork also edits (an import removed,
   a helper renamed, a struct literal rebuilt) replays cleanly and breaks only
   at compile time — or not at all.
4. Keep this file in sync: update the "Last rebased onto" marker and move
   entries between "Active" and "Superseded" as upstream evolves.

### The mandatory gates

```bash
cd backend && go build -tags exclude_frontend ./... && go vet -tags exclude_frontend ./...
pnpm install --frozen-lockfile
pnpm -C frontend run paraglide:compile
just format all
just lint all
just test backend && just test cli && just test types
```

Notes on each, all learned the hard way:

- `go vet` builds the **test** binaries, which `go build` does not. Upstream
  routinely drops DI nil checks from production code, which turns a fork test
  that passed `nil` collaborators into a package-wide SIGSEGV, and renames
  unexported test helpers without touching anything production code compiles
  against.
- The paraglide compile is not optional: without it every
  `#lib/paraglide/messages.js` import fails to resolve and `svelte-check`
  reports hundreds of phantom errors.
- `pnpm -C frontend run lint` (upstream's `shadcn/no-restyle` rule) catches
  typography and colour classes on `<Label>` that neither the build nor
  `svelte-check` sees.
- Run the **full** Go suite, not just the packages an entry lists: upstream's
  test suites exercise fork-modified helpers from the outside. Run it against
  pristine upstream too before treating a failure as the fork's — the set that
  fails on upstream alone in a container with no Docker daemon moves from cycle
  to cycle. The cheapest check is
  `git archive upstream/main | tar -x -C <tmpdir>` and running the same
  packages there.
- `golangci-lint` must be built with a Go toolchain at least as new as
  `backend/go.mod`'s, or it refuses the config outright. A distro binary is
  usually too old;
  `GOTOOLCHAIN=go<version> go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`
  works.

### The class of breakage no tool catches

An upstream commit that adds a **new reader** of a setting or helper whose
meaning the fork changed compiles, type-checks and passes every test while
behaving backwards. For every entry that reinterprets an existing field,
re-grep the tree for its consumers instead of trusting a clean replay. Entry
#12 carries the list of greps it needs.

---

## Active changes

### 1. "About this fork" README banner

- **Files:** `README.md`
- **What:** Prepend a blockquoted "About this fork" section above everything
  upstream's README opens with (currently a `> [!IMPORTANT]` feature-requests
  notice, then a `<div align="center">`) explaining that this is a dev fork,
  pointing at `FORK.md`, and listing the published image names and tags.
- **Why:** Makes the fork's purpose and image locations obvious to anyone who
  lands on the repo, and links to the change list.
- **Re-apply notes:** The fork banner goes first, upstream's notice second.
  Keep the one-sentence summary of carried changes in sync with the Active list
  below, dropping mentions of anything that moves to "Superseded".

### 2. Update contributor dev docs

- **Files:** `CONTRIBUTING.md`
- **What:** Split Prerequisites into Required (Docker, VS Code, Vite+,
  golangci-lint v2) and Optional (host tools for the Justfile shortcuts:
  `just`, `pnpm`/Node, Go 1.27+, with a Windows `winget` one-liner); clarify
  that Justfile recipes run on the host, not in the dev containers; and expand
  "Manual Commands" into three options (Vite+/Justfile on the host,
  `dev.sh shell`, and one-shot `docker compose exec` with `-p arcane-dev` and
  `pnpm -C frontend`).
- **Why:** Upstream's Prerequisites list only Docker, VS Code, Vite+ and
  golangci-lint with no optional host-tools guidance, so contributors who use
  the Justfile shortcuts or run checks manually hit missing host tools or the
  wrong Compose project name.
- **Re-apply notes:** Anchors on the `### Prerequisites`,
  `### Justfile Shortcuts` and `### Manual Commands` headings. Before
  re-applying, confirm the dev script path (`./scripts/development/dev.sh`) and
  the Compose project name — `PROJECT_NAME` in `dev.sh`, currently
  `arcane-dev`. Keep the Go toolchain line in step with `backend/go.mod`
  (currently `go 1.27.3`). Upstream owns the Required list; the fork only adds
  the Optional block, so drop any Optional bullet that becomes a duplicate of a
  Required one.
- **Redundancy check:** Upstream's Prerequisites still carry no optional
  host-tools guidance and its Manual Commands still omit `-p arcane-dev` —
  **keep**.

### 3. CI/workflows adapted for this fork

- **Files:** `.github/workflows/ci.yml`,
  `.github/workflows/build-next-images.yml`
- **Intent:** Make CI run on a fork without upstream-only infrastructure:
  - Run on the `main` branch (CI on push and PR; next images on push to
    `main`).
  - Use standard `ubuntu` runners instead of `depot-*` runners, and
    `docker/setup-buildx-action` instead of the depot buildx driver.
  - Drop steps that need secrets or licences unavailable on forks: the
    `depot/setup-action` steps, the depot registry mirror env, and the
    `cli-e2e-tests` job (depot runners plus an `E2E_ADMIN_STATIC_API_KEY`
    secret).
  - Narrow `permissions` to `contents: read`, and add a `github.ref` fallback
    to the concurrency group so push events group correctly.
  - Build and push with `docker/build-push-action` to
    `ghcr.io/<owner>/arcane` and `ghcr.io/<owner>/arcane-agent` (tag `next`)
    for `linux/amd64,linux/arm64` only.
- **Re-apply notes:** This is the highest-churn area on rebase. **Re-derive
  `ci.yml` from upstream's new file** and re-apply the transformations above,
  rather than keeping the fork's stale copy, and keep action and toolchain pins
  in sync with upstream. `build-next-images.yml` is a wholesale fork rewrite:
  re-derive it by hand and only sync upstream's action digests into it. The
  fork intentionally keeps the agent image named `arcane-agent` (not upstream's
  `agent`/`arcane-headless`) because it reads better when the repo owner is not
  "Arcane"; preserve published image names so existing pullers don't break.
  Verify the Dockerfile paths it references still exist (`docker/Dockerfile`,
  `docker/Dockerfile-agent`) and still take `VERSION`/`REVISION` build-args.
- **Redundancy check:** Upstream publishes images only from `release.yml` via
  GoReleaser Pro, Depot runners and cosign secrets, and its next-image workflow
  lives at `.depot/workflows/build-next-images.yml` — built on the same
  unavailable infrastructure, and at a path the fork's file does not collide
  with. Upstream's `ci.yml` still uses `depot-*` runners, the Depot CLI and the
  `cli-e2e-tests` job. The fork adaptation is still required — **keep**.
- **Out of scope:** `release.yml`, `codeql.yml`, `trivy.yml`, `pr-quality.yml`
  and everything under `.depot/` are left at upstream; the fork has never
  customised them.

### 4. GitOps manual sync: honest feedback and per-row spinner

- **Files:** `frontend/src/routes/(app)/gitops/components/sync-table.svelte`
- **What:** "Sync Now" reads `result.success` from the response body rather than
  treating any 2xx as a completed sync — an overlapping run is coalesced
  server-side and returns `success=false`. The UI shows `toast.success` (with
  the server's message as a description) only when the sync actually applied,
  and `toast.warning` with the server message otherwise. Loading state is
  tracked per row in a `SvelteSet` of row keys; each running row shows a spinner
  and "Syncing…" in its status column and disables only its own action, so
  independent syncs run concurrently.
- **Why:** Upstream fires a blanket `toast.success` on any 2xx and gates a
  single table-wide `isLoading.syncing` flag, so a coalesced no-op sync falsely
  reports success and there is no visible per-row progress.
- **Re-apply notes:** Anchors on `handlePerformSync`, the `isLoading` state, the
  `StatusCell` snippet and the two `RowActions` `DropdownMenu.Item`s (the table
  splits on `isBackup(sync)`, so **both** rows need the per-row state or a
  dangling `isLoading.syncing` reference is left behind). The spinner belongs in
  the status column, **not** in the `DropdownMenu.Item` — the menu closes on
  select, so a spinner there is never seen. The table is now environment-
  aggregated, so key the running set on `rowKey(item)`, not the bare `id`.
  `onSuccess(data)` depends on `handleApiResultWithCallbacks` passing the parsed
  response body. Depends on message keys `common_syncing`, `git_sync_success`,
  `git_sync_failed`, `backup_completed`, `backup_failed`.
- **Redundancy check:** Upstream still toasts success on any 2xx and uses the
  single `isLoading.syncing` flag with no status-column spinner — **keep**.

### 5. Short commit hash display with full hash on hover

- **Files:** `frontend/src/lib/utils/navigation.ts`,
  `frontend/src/routes/(app)/gitops/components/sync-table.svelte`,
  `frontend/src/routes/(app)/projects/[projectId]/+page.svelte`
- **What:** Add a `shortenGitCommit` helper (and `SHORT_GIT_COMMIT_LENGTH = 7`)
  beside the repository-URL helpers. Everywhere a GitOps commit hash is shown —
  the sync table's `CommitCell` and the project header — render the abbreviated
  hash with the full hash in a `title` tooltip. The commit link's `href` keeps
  the full hash so it resolves.
- **Why:** Full 40-character hashes are noisy in the UI; the short form reads
  better while the full value stays available on hover and in the link.
- **Re-apply notes:** The helper is appended to `navigation.ts` after the
  repository-URL helpers. Each display site derives
  `{@const shortCommit = …}`, renders it, and adds the `title`. Take the URL
  from whatever helper upstream currently exposes (`toGitRouteUrl(url, 'commit',
  hash)` today) rather than keeping a commit-only helper alive. Apply the
  treatment only where upstream itself renders a commit hash:
  `toGitRouteUrl`'s `tree`/`edit` callers render paths, not hashes, and are left
  alone. Shares `sync-table.svelte` with change #4 — apply both when redoing
  that file.
- **Redundancy check:** Upstream renders the raw full hash with no short form or
  `title` — **keep**.

### 6. Hide the copy button when the Clipboard API is unavailable

- **Files:** `frontend/src/lib/components/ui/copy-button/copy-button.svelte`
- **What:** Replace the `isSecure` gate (which rendered a disabled button with
  an "HTTPS required" tooltip) with a `canCopy` check
  (`typeof navigator !== 'undefined' && !!navigator.clipboard`) that hides the
  button entirely when the Clipboard API is unavailable — typically an insecure,
  non-HTTPS context. Removes the `{:else}` tooltip branch and its `Tooltip`
  import.
- **Why:** The Clipboard API is only exposed in secure contexts, so on insecure
  connections copying silently fails; offering a disabled button is confusing —
  better not to present an action that cannot work.
- **Re-apply notes:** `onMount` sets `canCopy`; `{#if canCopy}` wraps the single
  `ArcaneButton` and the disabled-button + `Tooltip` fallback is deleted. The
  `common_copy_https_required` message key becomes unused by this component but
  is **still shipped upstream** — leave the key in place, don't prune it.
  Upstream keeps reworking this component's markup, so re-apply by taking
  upstream's button body wholesale and re-deleting only the `{:else}` branch.
  Drop the now-unused `import * as Tooltip` line.
- **Redundancy check:** Upstream still renders the disabled-button-with-tooltip
  fallback keyed on `isSecure` — **keep**.

### 7. GitOps commit-hash injection into the synced project's env

- **Files:** `backend/pkg/projects/env.go` (including `EnvContentChanged`),
  `backend/pkg/projects/env_test.go`,
  `backend/internal/project/model.go` (the `GitOpsSync` model),
  `backend/internal/project/service.go` (the exhaustruct literal in
  `EnsureGitOpsProjectLinked`),
  `backend/internal/gitops/service.go` (the `CreateSync`/`UpdateSync` wiring),
  `backend/internal/gitops/children/sync/service.go` (the injection itself),
  `backend/internal/gitops/children/sync/service_test.go`,
  `backend/internal/database/migrations/00109_add_gitops_sync_inject_commit_env.go`,
  `types/gitops.go`, `frontend/src/lib/types/gitops.ts`,
  `frontend/src/lib/types/ui/gitops.ts`,
  `frontend/src/lib/components/dialogs/gitops-sync-form.svelte`,
  `frontend/src/lib/components/dialogs/gitops-sync-form-fields.svelte`,
  `frontend/messages/en.json`
- **What:** An opt-in `injectCommitEnv` flag on a GitOps sync. When it is on,
  the sync appends `ARCANE_GIT_COMMIT`, `ARCANE_GIT_COMMIT_SHORT` and
  `ARCANE_GIT_BRANCH` to the Git-sourced env content it hands to the existing
  three-file env merge (`.env.git` + `project.env` → `.env`), so compose can
  interpolate them and the `autoInjectEnv` setting can hand them to every
  service. `projects.BuildGitMetadataEnvContent` builds the block; the override
  builders (`BuildOverrideEnvContent`, `BuildAdditiveOverrideEnvContent`) skip
  the managed keys so a stale copy read back out of `.env` can never be pinned
  into the user's override; `projects.EnvContentChanged` ignores a _change in
  their values_ so a commit that leaves the synced files untouched does not
  redeploy a running project, while gaining or losing the keys (the toggle being
  switched on or off) still counts as a change so running containers pick up the
  new environment. Covers single-file, directory and swarm-stack syncs — all
  three funnel their Git env through one place.
- **Why:** The synced commit was already resolved, persisted
  (`gitops_syncs.last_sync_commit`) and shown in the UI, but nothing carried it
  into the container, so a deployed application could not report the commit it
  was built and deployed from.
- **Re-apply notes:**
  - **The migration.** Since upstream's `4cff77a8c` the migrations are **Goose
    Go migrations** under `backend/internal/database/migrations/`, with a
    v2.17.1 schema baseline at `00094`. On every rebase check whether upstream
    has claimed the fork migration's number and, if so, renumber it and update
    change #8. The fork migration **adds its column only when it is missing**
    (a `pragma_table_info` / `information_schema.columns` probe), which is what
    stops the next renumbering aborting startup with
    `duplicate column name: inject_commit_env`. Keep it that way; it is the one
    thing that bounds the fork's migration debt. Upstream's ceiling is `00108`.
  - **Injection has exactly two callsites**, both feeding
    `gitMetadataEnvContent`: `prepareSyncSource` (single-file and swarm, which
    both read `source.envContent`) and `syncProjectDirectory` (directory sync,
    which takes the commit hash as a parameter — upstream has ten test callsites
    to update alongside it). Metadata is appended _after_ the repo's own env so
    dotenv's last-assignment-wins rule keeps a repo-supplied key from spoofing
    it.
  - **The change-detection half** lives in `projects.EnvContentChanged`
    (`backend/pkg/projects/env.go`), whose callers are
    `ApplyGitSyncProjectFiles` and `syncProjectDirectory`. Re-derive the
    git-metadata exemption into whatever function upstream currently owns the
    comparison with, rather than keeping a fork-local copy, and keep
    `TestEnvContentChangedInjectedCommitMetadata` beside it.
  - **The API contract is one field.** Since `68d21f11c` upstream drives
    create and update from a single `types.GitOpsSync` plus a `Submitted` map,
    so the fork adds `InjectCommitEnv` once there, one line to the `CreateSync`
    record literal and one row to `UpdateSync`'s column map. Add it to
    `GitOpsSyncWritable` in `frontend/src/lib/types/ui/gitops.ts` too, or
    `svelte-check` rejects the payload.
  - **`EnsureGitOpsProjectLinked` has an `//exhaustruct:enforce` `GitOpsSync`
    literal.** A new model field must be listed there or `golangci-lint` fails —
    nothing else catches it.
  - **The dialog half** is one field in the form schema, one in the defaults,
    one in the payload and one switch block. Upstream keeps restructuring that
    form (it is now split across `gitops-sync-form.svelte` and
    `gitops-sync-form-fields.svelte`), so re-apply by adding the field to
    whatever object upstream currently returns and by copying a neighbouring
    switch's markup and classes verbatim, never by keeping the fork's copy of
    the surrounding form.
  - **Known corner:** disabling the flag drops the keys on the next sync unless
    the repository ships no `.env` of its own — there nothing replaces the
    Git-sourced env, so the last injected values stay in the project's `.env`
    until removed by hand.
- **Redundancy check:** Upstream has no commit-injection option; its GitOps sync
  writes only the repository's own env content — **keep**.

### 8. One-time repair for databases migrated by a pre-renumber fork build

- **Files:** `backend/internal/database/forkmigration.go`,
  `backend/internal/database/forkmigration_test.go`, plus a one-line hook in
  `backend/internal/database/database.go`
- **What:** Before the Goose run, detect a database that applied change #7's
  migration under one of its _old_ numbers and replay the upstream migration
  that number now owns. Goose keys its bookkeeping on the version number alone,
  so on such a database the upstream migration is silently treated as applied
  and skipped.
- **Why this is now small.** Upstream's `4cff77a8c` introduced a **v2.17.1
  schema baseline at `00094`** and `migrateDatabase` refuses any database whose
  recorded version is below it, telling the operator to upgrade through v2.17.1
  first. For a fork install that means a pre-v3 fork build, whose own repair has
  already lifted every older era to 95. So the eight eras this entry used to
  carry (69, 71, 73, 74, 77, 85, 88, 90) are **unreachable** and were dropped.
  Two remain:
  - **Era 94** — the fork migration took `094`, so upstream's own `094`
    (`image_updates.latest_seen_at`, plus the backfill stamping rows already
    flagged `has_update`) was skipped, and the `00094` baseline that would
    otherwise have created the column is skipped too. The repair adds the
    column and runs the backfill.
  - **Era 95** — the fork migration took `095`, so upstream's `00095`
    (deleting the two retired `*/30`-second schedule defaults) is skipped. Left
    alone, `00101` would rewrite those values to five-field form and keep them
    instead of letting them fall back to the current default. The repair runs
    the delete.
- **Re-apply notes:** `gitops_syncs.inject_commit_env` is the fingerprint: a
  pristine upstream database at the same version does not carry it and must not
  be touched. Every statement is idempotent, so the repair running outside
  Goose's Postgres session lock is harmless — a second instance re-runs the same
  checks and finds nothing to do. **On every rebase, check the migrations
  _above_ each orphaned number, not just the one that claimed it**: a later
  upstream migration that depends on the skipped one has to be considered, and
  that is what decides whether a replay must run before the Goose `UpTo` (the
  current two are both self-contained, so a single pre-`UpTo` pass suffices).
  When upstream claims the fork migration's number again, add the new era here
  and keep the fork migration's own column probe — the probe is what makes the
  rest of this file small.
- **Redundancy check:** Fork-only debt by construction; it can be deleted once
  no database migrated by a pre-`00109` fork build is left in the lab. It is the
  one entry in this file that is **not** upstreamable.

### 9. E2E test images pulled from a mirror, with retries

- **Files:** `.github/workflows/ci.yml`, `tests/setup/project.data.ts`,
  `tests/setup/global-setup.ts`,
  `tests/setup/projects/test-project-static/compose.yaml`,
  `tests/setup/gitops-test-repo/compose.yaml`, `tests/spec/projects.spec.ts`,
  `tests/spec/images.spec.ts`
- **What:** The nginx E2E test image comes from `mirror.gcr.io` (Google's
  anonymous Docker Hub pull-through cache) instead of `public.ecr.aws`, every
  prefetch pull retries three times with linear backoff, and the dead
  `docker save … > /tmp/test-images.tar` is dropped — nothing has ever read that
  tarball; the pull alone is what seeds the runner's image store. The playwright
  browser image comes from the public `mcr.microsoft.com` default rather than
  upstream's depot registry mirror, which a fork has no token for. Busybox,
  `cgr.dev/chainguard/minio` and `ghcr.io/rustic-rs/rustic` stay on their
  upstream registries.
- **Why:** `public.ecr.aws` rate-limits anonymous pulls per source IP, and the
  E2E matrix jobs pull the same image simultaneously from one runner, so
  `toomanyrequests: Rate exceeded` failed E2E jobs repeatedly before any test
  ran.
- **Re-apply notes:** Upstream-agnostic and worth upstreaming. **On every
  rebase, re-grep the whole `tests/` tree for `nginx:stable-alpine`** — new
  upstream fixtures are the usual way a pull drifts back onto ECR, and three
  rebases in a row added a site. But the answer is not automatically "move it":
  only a reference that causes a **pull** belongs on the mirror. The current
  pull sites are the five fixtures above plus the workflow prefetch;
  `TEST_IMAGE_REFS.nginx` in the "Update checks" block of `images.spec.ts` and
  the ECR names in `updates.spec.ts` / `image-updates.spec.ts` only resolve
  registry metadata and are deliberately left alone. The `images.spec.ts`
  "Pull Image" dialog spec types a reference by hand and pulls it live, so its
  alpine is on the mirror too. Before assuming a pull failure is transient,
  verify `mirror.gcr.io` still serves `library/nginx:stable-alpine` and
  `library/alpine:3.20` anonymously (manifest _and_ blobs). Verified at this
  rebase: HTTP 200 for both.
- **Redundancy check:** Drop if upstream moves these pulls off `public.ecr.aws`
  itself. It hasn't — upstream's `ci.yml` still prefetches from ECR and still
  writes the dead tarball — **keep**.

### 10. Quieter startup: stop logging non-problems

- **Files:** `backend/internal/apikey/service.go`
- **What:** "Default admin user not found" and "not a global admin" in
  `getDefaultAdminUser` describe a normal install whose administrator does not
  use the built-in account, so they drop from WARN to DEBUG. The actionable
  case — a configured static admin API key with no eligible account to attach
  to — gets its own WARN in `ReconcileDefaultAdminAPIKey`, where the raw key is
  in scope.
- **Why:** A healthy lab install's startup logs were full of warnings about
  non-events, burying the messages that matter.
- **Re-apply notes:** Anchors on `getDefaultAdminUser` and
  `ReconcileDefaultAdminAPIKey`. This entry used to carry five more bullets;
  all five are gone — see "Superseded / now upstream". Each piece stands alone,
  so check them individually rather than the entry as a whole.
- **Redundancy check:** Upstream still logs the admin-user line at WARN
  unconditionally — **keep**.

### 11. Convert a Git-synced project back into a regular project

- **Files:** `backend/internal/gitops/service.go`,
  `backend/internal/gitops/handler.go`, `backend/internal/gitops/module.go`,
  `backend/internal/gitops/service_test.go`,
  `backend/internal/project/service.go`,
  `backend/internal/project/service_test.go`,
  `backend/internal/scheduler/entityjobs/entityjobs.go`,
  `backend/pkg/agent/commands.go`,
  `frontend/src/lib/services/gitops-sync-service.ts`,
  `frontend/src/routes/(app)/projects/[projectId]/+page.svelte`,
  `frontend/messages/en.json`
- **What:** Two halves. (1) `POST
  /environments/{id}/gitops/syncs/{syncId}/detach` →
  `GitOpsSyncService.DetachManagedProjects`, which turns every project a sync
  manages back into a regular project: it clears `projects.gitops_managed_by`,
  clears the sync's `project_id`, switches `auto_sync` off and unregisters the
  recurring job, while leaving the project's files and containers and the sync's
  repository binding alone. The DB write is
  `ProjectService.ReleaseGitOpsProjectLinks`, the deliberate mirror of
  `EnsureGitOpsProjectLinked`, so the project domain keeps ownership of its own
  compose cache and events. Surfaced as a confirmed "Convert to regular project"
  button beside "Sync from Git" in the read-only Git banner, requiring
  `gitops:update` **and** `projects:update`. (2)
  `SyncProjectsFromFileSystem` calls `clearOrphanedGitOpsLinks`, one UPDATE
  releasing any project whose `gitops_managed_by` names a sync row that no
  longer exists.
- **Why:** `gitops_managed_by` is the only thing that marks a project as
  Git-managed, and nothing could clear it. Until upstream `775024e` (first
  shipped in 2.8.0) deleting a sync did not clear it, so any database that
  deleted a sync on an older build carries projects that are permanently
  read-only — compose file, project name and workspace files all locked, exempt
  from filesystem cleanup, and with no UI path to clear the flag because the
  sync it names is gone. Even on current builds, unmanaging a project meant
  deleting its sync and, since `DeleteRepository` refuses while any sync
  references it, unwinding the repository too.
- **Re-apply notes:** Two invariants are the whole point of the design, and both
  were found by review after a first attempt put the operation in the project
  domain:
  1. **Hold the sync's admission lease across the release.** `PerformSync`
     acquires the lease and _then_ loads the sync row, so a run already past
     admission holds the `ProjectID` it loaded. Without the lease a detach can
     commit underneath it and the run will mirror repository files over the
     now-regular project and re-link it — either via
     `EnsureGitOpsProjectLinked` (whose guard passes once `gitops_managed_by`
     is nil) or via the directory sync's unconditional write. Refusing with
     `common.ErrConflict` (→ 409) while a run is in flight is what makes the two
     mutually exclusive. This is why the operation lives in the GitOps domain:
     it is the side that owns the lease, and `internal/gitops` already imports
     `internal/project`, so the dependency cannot go the other way.
     `entityjobs.Registry` has no non-blocking admission of its own, so the fork
     adds `TryAcquire` beside its `Acquire` — the same one-line shape, because
     a detach must refuse rather than queue.
  2. **Unregister the job explicitly.** A scheduled run returns early on
     `AutoSync=false` but does **not** unregister itself — only the
     missing-row path does. The detach writes `auto_sync=false` directly in SQL,
     so it still has to remove the job itself; setting the flag alone leaves a
     job firing and re-reading the row on every interval until restart.
     `auto_sync=false` is only for durability across restarts, since startup
     registers auto-sync rows only.

  A missing sync row needs no lease — nothing can run for it — and must still
  release its projects; that tolerance is what unsticks pre-2.8.0 databases, and
  it mirrors `DeleteSync`, which is likewise deletable when its row cannot be
  loaded. New project or sync routes also need a `commandRoutes` entry in
  `backend/pkg/agent/commands.go` or agent-tunnel environments cannot reach
  them. The route follows upstream's own convention for POST sub-paths on a sync
  (`handlerutil.RegisterSecured` with `handlerutil.Operation`, as
  `backup/resolve` does). The button belongs in whatever action row upstream's
  read-only Git banner currently renders, and takes that row's conventions. No
  migration: the orphan reconcile is idempotent code on the project filesystem
  sync, which keeps this out of the fork's migration debt. Verify with
  `go test ./internal/gitops/ -run DetachManagedProjects`,
  `go test ./internal/project/ -run 'ReleaseGitOpsProjectLinks|ClearsOrphanedGitOpsLink'`
  and `pnpm -C frontend check`.

- **Redundancy check:** Upstream has no detach/convert action and no orphan
  reconcile — **keep**. Upstream keeps moving adjacent without covering either
  half: `DeleteSync` clears `gitops_managed_by` even when the sync row can't be
  loaded, startup cleanup releases syncs whose _environment_ is gone, and the
  pre-deploy path treats a stale marker as unmanaged — but nothing repairs a
  project whose `gitops_managed_by` names a sync that was _already deleted_, and
  nothing converts a managed project back to a regular one. Drop the reconcile
  half if upstream adds its own cleanup for links to deleted syncs; drop the
  whole entry if upstream ships a way to unmanage a GitOps project.

### 12. Include-mode switch for automation container lists

- **Files:** `backend/internal/settings/container_filter.go`,
  `backend/internal/settings/container_filter_test.go`,
  `backend/internal/settings/model.go`,
  `backend/internal/settings/service.go`,
  `backend/internal/settings/service_test.go`,
  `backend/internal/updater/service.go`,
  `backend/internal/container/service.go`,
  `backend/internal/scheduler/auto_heal_job.go`,
  `backend/internal/scheduler/auto_heal_job_test.go`,
  `backend/internal/bootstrap/jobs_bootstrap.go`,
  `backend/internal/config/schema/schema.go`,
  `backend/internal/config/schema/schema_test.go`,
  `frontend/src/lib/components/settings/environment/automations-tab.svelte`,
  `frontend/src/lib/components/settings/environment/environment-form-schema.ts`,
  `frontend/src/routes/(app)/settings/automations/+page.svelte`,
  `frontend/src/lib/types/settings.ts`, `frontend/messages/en.json`
- **What:** Every environment automation that carries an "Excluded Containers"
  list — auto-update and auto-heal — gets an "Include mode" switch (new boolean
  settings `autoUpdateIncludeMode` and `autoHealIncludeMode`, both defaulting to
  `false`) that inverts the list into "Include Containers": only the listed
  containers are affected by the automation and every other container is
  excluded. The names stay in the existing `autoUpdateExcludedContainers` /
  `autoHealExcludedContainers` CSVs; the mode only changes how the list is
  interpreted, so there is no migration and existing installs behave exactly as
  before. The settings card flips its heading and description with the mode, and
  the resolved `autoUpdateEnabled` the backend reports for container details and
  summaries follows it, so every surface showing the ignored/enabled state does
  too.
- **Why:** In a lab where only a handful of containers should auto-update or
  auto-heal, an exclusion list has to be re-edited every time a container is
  added; include mode expresses "only these" directly as an allowlist.
- **Re-apply notes:** **The inversion lives in exactly one place:**
  `settings.ContainerAutoUpdateFilter`
  (`backend/internal/settings/container_filter.go`), reached through
  `SettingsService.ContainerAutoUpdateFilter`, which reads the list and the mode
  together so the two cannot drift apart at a call site. Add a consumer by
  calling that, never by parsing the CSV again. It sits in the settings domain
  because the Docker helpers it wraps (`ExcludedContainerNameSet`,
  `ContainerNameExcluded`) live in the external `kit.getarcane.app/docker`
  module, which a fork cannot extend; the settings domain owns the setting and
  every consumer already holds a settings service. Its **nil-receiver guard**
  returns a zero filter that excludes nothing — do not "tidy" that away to match
  upstream's style; it is what lets every caller-side nil guard be dropped as
  upstream removes its own.

  The embedded updater engine (`kit.getarcane.app/updater`) only understands an
  _exclusion_ list through its `SettingsProvider.ExcludedContainers` port, so in
  include mode Arcane's implementation of that port
  (`UpdaterService.ExcludedContainers`) materializes the inverse — the names of
  all Docker containers _not_ on the include list. Drop that materialization if
  the engine ever grows a native include mode.

  Arcane-internal filtering uses the filter directly: both used-image collection
  paths in the updater service (direct containers and Compose-project
  containers, or "include none" is not a true no-op), the resolved
  `autoUpdateEnabled` on container details and summaries
  (`internal/container/service.go`), and `parseContainerFilter` in the auto-heal
  job, which keeps its own parse because auto-heal has its own list and mode.
  Each of those sites also honours upstream's `ignoreOptOuts`, so a manual run
  still bypasses the list in both modes.
  `SetContainerAutoUpdateExclusion` inverts its add/remove in include mode so
  "enable auto-update" always means "make this container updatable" regardless
  of mode.

  **This is the entry's main maintenance cost.** Any new upstream consumer of
  `autoUpdateExcludedContainers` has to be made mode-aware the same way, or
  include mode silently inverts for it — and nothing in `git`, the build, the
  type-check or the tests catches that. On every rebase re-grep for
  `autoUpdateExcludedContainers`, for `ExcludedContainerNameSet` /
  `ContainerNameExcluded`, and for implementations of the updater engine's
  `SettingsProvider` port. At this rebase the greps came back clean apart from
  the three known consumers and one upstream test
  (`internal/imageupdate/service_test.go`) that asserts the opposite of a
  consumer — "the UI exclusion governs installation only; it must not reach the
  checker" — so there is nothing there to make mode-aware.

  Semantics to preserve: include mode with an empty list affects **no**
  containers (the list is the allowlist), and the
  `com.getarcaneapp.arcane.updater=false` label still overrides the list in both
  modes. A new setting key touches more than the model struct: `overrideDocRules`
  and `expectedSettingOverrideKeys` in the config schema (+test), the reschedule
  subscription key list in `jobs_bootstrap.go` for auto-heal, the Zod form
  schema, and the `keys` array on the settings page that owns the tab. Defaults
  now come from the model's `default:` struct tags, so there is no separate
  defaults list to update.

  The frontend half is the settings Automations tab. Upstream's settings UI is
  built from `SettingsRow` components, so the switch is its own
  `layout="switch"` row above the container list, and the list row's `label` and
  `description` are mode-aware. Copy a neighbouring row's props, bindings and
  classes verbatim rather than keeping the fork's markup: upstream keeps
  changing both, and its `shadcn/no-restyle` rule rejects typography and colour
  classes on `<Label>` — only `pnpm -C frontend run lint` catches that.
  Verify with
  `go test ./internal/settings/... ./internal/updater/... ./internal/container/... ./internal/scheduler/... ./internal/config/...`,
  `pnpm -C frontend check` and `pnpm -C frontend run lint`.

- **Redundancy check:** Upstream's automation container lists are exclusion-only
  with no inversion switch — **keep**. Drop if upstream ships its own
  include/allowlist mode for these automations (watch for a rename of the
  `*ExcludedContainers` settings or a mode setting beside them), or if
  `kit.getarcane.app/updater` grows a native include mode — then the
  materialized inverse can go with it. Several of the fork's inversions have
  been dropped over time because upstream stopped consulting the list in those
  paths at all; check each consumer individually rather than the entry as a
  whole.

---

## Superseded / now upstream

Dropped this cycle (the v2.17.1 → v3 rebase, upstream `b8c837ddf`):

- **Watcher exclusions for Arcane's own scratch and snapshot directories**
  _(was one of change #10's bullets: `backend/pkg/fswatch/watcher.go`)_ —
  upstream's `f6aad8689` replaced `pkg/fswatch` with the external
  `kit.getarcane.app/tripwire` module and configures it with
  `SkipDir: projects.IsIgnoredProjectDirName`, which is exactly
  `IsInternalScratchDirName || IsFilesystemSnapshotDirName` — the two
  predicates the fork's exclusion used. Its `Key` predicate also rejects any
  path with an ignored segment. Upstream now does what this bullet did.
- **Unreadable-directory watch failures at DEBUG instead of WARN** _(was one of
  change #10's bullets)_ — the same `f6aad8689` moved the watcher into
  `kit.getarcane.app/tripwire`, which logs
  `tripwire: skipping unreadable directory` at WARN from inside a module the
  fork cannot patch. Suppressing it through `tripwire.Config.Logger` would be a
  log-rewriting shim, the opposite of this entry's "fix it at the source"
  intent, so the bullet is dropped. If it still matters, it belongs upstream in
  the kit module, not here.
- **Per-field `Update` DTO entries for the new settings keys** _(was part of
  change #12: `types/settings/settings.go`)_ — upstream's `94c1413a0` flattened
  the `types/` module and replaced the per-field settings update DTO with
  `types.SettingsUpdate map[string]string`, so a new setting key needs no DTO
  field at all.
- **Explicit defaults in `DefaultSettingsConfig`** _(was part of change #12)_ —
  upstream now derives every default from the model's `default:` struct tag, so
  the fork's two entries in the defaults list have nothing to add to.
- **Separate `CreateSyncRequest` / `UpdateSyncRequest` fields** _(was part of
  change #7: `types/gitops/gitops.go`)_ — upstream's `68d21f11c` drives both
  from one `types.GitOpsSync` plus a `Submitted` map, so the fork's three
  request fields collapsed into one.
- **The eight pre-94 migration-renumber eras** _(was the bulk of change #8:
  eras 69, 71, 73, 74, 77, 85, 88, 90)_ — upstream's `4cff77a8c` added a
  v2.17.1 schema baseline at `00094` and `migrateDatabase` now refuses any
  database recorded below it, so none of those eras can reach the repair; they
  must pass through a pre-v3 fork build, whose own repair already lifts them to 95. Roughly 1,900 lines of repair and tests went with them.

Dropped in earlier cycles:

- **Deploy falls back to build when a build-capable service's image can't be
  pulled** — upstream's `a78129075` rewrote the `buildEnabled` branch of
  `DecideDeployImageAction` to set `FallbackBuildOnPullFail` for the cases the
  fork patched, and deleted the two dead arms the entry worked around.
- **Shallow, tag-less GitOps clones and an `ls-remote` connection test** —
  upstream adopted both in `pkg/gitutil`.
- **Exclude nested build artifacts from the Docker build context** — upstream
  took the same `.dockerignore` entries.
- **Quiet the "Job rescheduled" log for a never-scheduled disabled job** —
  upstream's jobs fan-out refactor deleted the log line outright.
- **Include-mode inversion in update discovery** and **in the container
  tag-scan exclusion port** _(parts of change #12)_ — upstream stopped
  consulting the exclusion list in those paths at all.
- **Client-side auto-update ignored state** _(part of change #12)_ — upstream's
  `aaac10a0d` moved that resolution onto the server; the fork reads
  `container.autoUpdateEnabled` instead. Do not re-introduce it.
- **`PathMapper` identity-mount warning** and **the legacy `users.roles`
  backfill log** _(parts of change #10)_ — fixed upstream independently.
- **Preinstall Bun in the dev Dockerfile**, **set background early to prevent
  white flash**, **surface the real rejection reason on a tunnel register-send
  race**, **sanitize discovered project names**, **keep LF line endings for
  scripts**, **user avatar 404 when no email is set**, and **toast on project
  form validation failure** — all implemented upstream.
