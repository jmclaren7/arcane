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
and the dev container work outside upstream's environment (changes #2, #3, #5,
#12); **documentation** describing the fork itself and filling gaps in the
contributor setup docs (#1, #4); and a small set of **behavioural fixes and
UI refinements** that are genuinely upstreamable but haven't been submitted or
merged yet (#6–#10, #13–#15) — plus one piece of fork-only debt, the
migration-renumbering startup repair (#11). The fork deliberately carries no product features of
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

> **Last rebased onto upstream:** `b8bc5b4f` — _release: 2.8.0_, on
> 2026-08-15. _(Previously `5c3c7b70`, 2026-08-08.)_
>
> This rebase carried 73 new upstream commits: the 2.8.0 release, a
> reorganization of `backend/internal/services` into per-domain packages
> (`1cea5f48` — the single biggest structural change this fork has crossed),
> a consolidation of backend file handling onto `go.getarcane.app/acfs`
> (`bcdbe817`, `3d7e9ae2`), a frontend-toolchain migration to Vite+ / `vp`
> with a root pnpm workspace (`4ca81d7c`, `9e2fe2f2`), the volume-workspace
> feature (`6f6e2e2a`) and project tags (`9687b138`) — which claimed
> migration numbers **071 and 072** — upstream's own white-flash fix
> (`a6801855`), a performance sweep (config/materialization, GPU stats,
> compose CLI reuse, API-key caching), and the usual dependency bumps and
> Crowdin updates. The headline outcomes:
>
> 1. **Change #2 (early background to prevent white flash) superseded.**
>    Upstream's `a6801855` injects an equivalent bootstrap script into
>    `app.html` (reads `mode-watcher-mode`, toggles the `dark` class, sets
>    `colorScheme` pre-hydration) plus appearance-attribute handling the fork
>    never had. Resolved in favour of upstream; the fork commit was dropped
>    outright and the entry moved to "Superseded / now upstream". **All
>    following entries were renumbered down by one** (old #3–#15 are now
>    #2–#14), matching the gap-closing convention from the `60a8e663` rebase.
> 2. **The fork migration renumbered again, `071` → `073`,** because
>    upstream's volume-workspace and project-tags work claimed 071/072. This
>    re-created the exact collision change #11 exists for, one number later:
>    a lab database migrated by a 071-era fork build has version 71 recorded
>    for the *fork's* migration, so upstream's real 071 (volume-workspace
>    legacy-key renames) would be silently skipped and 073 would abort on the
>    duplicate `inject_commit_env` column. Change #11's repair was extended
>    with a third constant (`forkCommitEnvMidRenumberVersion = 71`) and a
>    replay of upstream 071's (idempotent) rename statements when it detects
>    that state; both new shapes are covered by tests. Lab instances upgrade
>    through one repaired startup with no hand-fix.
> 3. **Changes #10, #11 and #13 relocated.** The services reorg moved
>    `gitops_sync_service.go` → `internal/gitops/gitops_sync.go`,
>    `api_key_service.go` → `internal/apikey/service.go` and
>    `role_service.go` → `internal/role/service.go`; git followed the renames
>    and replayed the fork hunks into the new files nearly clean (one
>    signature conflict in `stageDirectorySyncInternal`, where upstream had
>    renamed `getProjectsDirectoryInternal` → `GetProjectsDirectory`; one
>    error-constant conflict in the apikey service, `ErrUserNotFound` →
>    `common.ErrUserNotFound`). Change #14's consumer moved to
>    `internal/project/project_lifecycle.go` with no re-work needed.
> 4. **Change #5 (fork CI) re-derived onto upstream's Vite+ CI.** Upstream's
>    `type-check` and E2E jobs now use `voidzero-dev/setup-vp` and `vp -C
>    tests exec playwright`; the fork adaptation keeps its push/`main`
>    triggers, `contents: read` permissions, `github.ref` concurrency
>    fallback, `ubuntu-latest` runners, `docker/setup-buildx-action`, and the
>    change-#12 mirror/retry prefetch, while adopting upstream's new steps
>    (including the Playwright cache key moving to the root
>    `pnpm-lock.yaml`). Change #4 (CONTRIBUTING) was merged around upstream's
>    Vite+ rewrite of the same sections, keeping the fork's three-option
>    Manual Commands structure with Vite+/Justfile as the host option.
>
> Change #8 replayed clean even though upstream rewrote the project detail
> page around the new workspace UI — its header commit-hash site auto-merged
> at the new location, and the short-hash treatment was confirmed in place.
> Changes #2, #3, #6, #7, #9, #12 and #14 replayed with zero conflicts and
> their redundancy checks were re-verified against `b8bc5b4f`.
>
> Verified post-rebase: `GOEXPERIMENT=jsonv2 go build ./...` and
> `go vet ./...` clean over the whole backend; `go test
> ./internal/database/... ./pkg/projects/... ./pkg/gitutil/...
> ./pkg/fswatch/... ./pkg/libarcane/edge/... ./internal/gitops/...
> ./internal/apikey/... ./internal/role/...` all passing (including the new
> mid-renumber repair tests); and `pnpm -C frontend check` (root workspace
> install) reporting 0 errors / 0 warnings. Of the 73 commits, one
> (`a6801855`) superseded an active fork change; the remaining 14 active
> changes are all still necessary.
>
> Earlier rebase (`5c3c7b70` — _refactor: use proper tanstack-table v9
> logic_, 2026-08-08; previously `60a8e663`, 2026-08-05):
> carried 27 new upstream commits: the 2.7.0 release, a large
> UI-layout/view-consolidation refactor (`6422c76`, which reworked detail
> pages around shared `resource-detail` components, replaced the project
> containers table with a services panel, dropped the project Logs tab, and
> moved the ports page under networks), a tanstack-table v9 logic refactor
> (`5c3c7b7`), a passkey-identity fix series (`3b0fc6a` moving iOS-app
> passkey logic to the backend, plus `1ef7cc5`, `bfc35fb`, `b84a386`,
> `6b92201`), E2E spec stabilizations, and the usual dependency bumps and
> Crowdin updates. One conflict needed manual work:
>
> 1. **Change #9 partially superseded.** Upstream's `6422c76` removed the
>    commit-hash display from the project page's read-only git-managed alert
>    (the alert now shows only the pre-deploy hook line and the env note)
>    while keeping the header display. The fork's short-hash treatment of the
>    alert site was dropped in favour of upstream's removal; the header and
>    sync-table sites auto-merged cleanly and keep the short hash with the
>    full hash in `title`. The entry below now lists only the two surviving
>    display sites.
>
> **Change #14 was documented this round** — the startup-log noise fix
> (`af9f77c`) had been committed to the fork on 2026-08-05 but never entered
> in this file. It replayed with zero conflicts (upstream touched none of its
> files this round) and is now recorded as Active change #14.
>
> Upstream added no new migrations (`071` still stands for change #11 and
> change #12's constants are unchanged) and did not touch
> `.github/workflows/ci.yml`, `.depot/workflows/ci.yml`, or any other file
> carrying changes #2–#8, #10, #12, #13, so none needed re-derivation this
> round. Upstream's E2E spec stabilizations (`6422c76`, `0b5eecf`) edited
> `tests/spec/{project,images}.spec.ts` away from the lines change #13
> touches, and merged clean.
>
> Verified post-rebase: `GOEXPERIMENT=jsonv2 go build ./...` and
> `go vet ./...` clean over the whole backend; `go test ./pkg/projects/...
> ./pkg/gitutil/... ./pkg/libarcane/edge/... ./pkg/fswatch/...
> ./internal/database/...` and the GitOps service tests passing; and
> `pnpm -C frontend check` reporting 0 errors / 0 warnings. Of the 27
> commits, one (`6422c76`) partially superseded an active fork change (#9's
> alert-site hunk); all 14 active changes remain necessary (redundancy checks
> re-verified against `5c3c7b70`).
>
> Earlier rebase (`60a8e663`, 2026-08-05; previously `c9fa64b`, 2026-08-03):
> carried 19 new upstream commits: a compose-interpolation
> isolation fix (`b475a332`, which stops Arcane's own process environment
> leaking into managed projects), an effective-`.env` rewrite that updates
> overridden keys in place instead of appending duplicates (`d4480b41`),
> custom-payload generic webhooks and Google Chat notifications (`20d0f33a`),
> a docker/compose v5.4.0 bump with gated diverged-volume recreation
> (`a33f047f`), password-policy enforcement on all password paths
> (`e1c68d9e`), a default-admin-role scoping fix (`f07ceff9`), stale
> bootstrap-API-key cleanup (`ef7814b9`), a non-HTTPS bulk-remove fix
> (`1a1d392a`), a sheet-panel/copy-button animation fix (`5d6c07b2`), CLI
> riscv64 self-update, dependency bumps, and Crowdin updates. Two things needed
> manual work:
>
> 1. **The tunnel register-send race fix (then #11) dropped — upstream
>    implemented it.** `ccf4bd87` (nominally a grpc `1.82.1`→`1.83.0` bump)
>    carries the same `io.EOF` fall-through in `serveTunnelSessionInternal`,
>    gated slightly more tightly than the fork's
>    (`conn.Transport() != EdgeTransportGRPC || !errors.Is(err, io.EOF)`, so
>    the fall-through applies only to the gRPC transport). Resolved in favour
>    of upstream and moved to "Superseded / now upstream". **The GitOps
>    commit-injection entry was renumbered #12 → #11 to close the gap**, so
>    references to "#12" in the earlier-rebase notes below mean today's #11.
> 2. **Change #10 conflicted** with upstream's `5d6c07b2` copy-button
>    animation rework (`grid place-items-center` wrapper, `col-start-1
>    row-start-1` and `out:scale` on each icon branch). Upstream's restructured
>    markup was kept wholesale inside the `{#if canCopy}` branch and only the
>    fork's deletion of the `{:else}` disabled-button + `Tooltip` fallback was
>    re-applied.
>
> **Change #11 replayed with zero conflicts but needed semantic review**, since
> upstream's `d4480b41` rewrote `BuildEffectiveEnvContent` in the same file to
> merge overrides in place via a new `mergeEnvOverridesInPlaceInternal`. The
> two are compatible: the in-place rewrite only touches Git-content lines whose
> key appears in the *override*, and change #11 already filters the managed
> `ARCANE_GIT_*` keys out of both override builders, so a metadata line can
> never be rewritten from a stale override. Upstream also merged the
> `ProjectEnvMode*` constants into the top `const` block and dropped the
> process-env snapshot helpers; the fork's metadata `const` blocks and helpers
> replayed around those cleanly. Upstream added **no new migrations**, so the
> fork's `071_add_gitops_sync_inject_commit_env.sql` keeps its number and the
> `069`/`070` deployment caveat from the previous rebase does not recur.
> Upstream touched only `release.yml` in `.github/workflows/` (`60a8e663`,
> `run_install: true`), which the fork leaves at upstream, so change #6 needed
> no re-derivation this round.
>
> Verified post-rebase: `GOEXPERIMENT=jsonv2 go build ./...` and
> `go vet ./...` clean over the whole backend, `go test ./pkg/projects/...
> ./pkg/gitutil/... ./pkg/libarcane/edge/...` passing (including the fork's
> `TestBuildGitMetadataEnvContent` alongside upstream's new in-place-rewrite
> cases in `TestBuildEffectiveEnvContent`), all 43 GitOps service tests
> passing, and `pnpm -C frontend check` reporting 0 errors / 0 warnings. Two
> `internal/services` tests
> (`TestProjectService_UpdateProject_AllowsRenameAfterJournalRecoveryDockerUnavailable`,
> `TestProjectService_ListProjects_WithDerivedStatusFilter_AllowsAllPageSizeSentinel`)
> fail for want of a Docker daemon; both were confirmed to fail identically on
> pristine `upstream/main` in the same container, so they are environmental,
> not fork regressions. Note that `go build`/`go vet` over the whole backend
> also needs `backend/frontend/dist` to exist (it is gitignored and generated
> by the frontend build) — without it, `frontend.go`'s `//go:embed all:dist`
> fails on upstream too; a stub `dist/index.html` is enough to gate the build.
> Of the 19 commits, one (`ccf4bd87`) superseded an active fork change; the
> remaining 11 changes are all still necessary (redundancy checks re-verified
> against `60a8e663`).
>
> Earlier rebase (`c9fa64b`, 2026-08-03): carried 64 new upstream commits, including several structural
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
>    **Superseded on 2026-08-05 by change #12**, which repairs such a database
>    automatically at startup. Do not follow the hand-fix above: renaming the
>    row to 71 on its own raises the Goose version straight to the target, so
>    upstream's registry-names `069` stays unapplied — and on a database that
>    had not yet reached 70, the passkey `070` is skipped too.
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

### 2. Preinstall Bun in the dev Dockerfile

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
  `frontend/package.json` (the Vite+ migration moved `dev`/`build`/`format`
  to `vp` but left `check` on `svelte-check-rs`) and upstream's
  `frontend-dev` stage still omits Bun (the `ca-certificates`/`curl` install
  lives in the separate `backend-dev` stage, not the frontend one) —
  **keep**.

### 3. Exclude nested build artifacts from the Docker build context

- **Files:** `.dockerignore`
- **What:** Add recursive `**/node_modules` and `**/.svelte-kit` alongside the
  existing top-level entries, and list the JS workspaces' `build/` directories
  explicitly (`build`, `frontend/build`, `tests/build`,
  `email-templates/build`) instead of recursively.
- **Why:** Nested workspace folders were copied into the build context and
  caused build failures (e.g. on Windows, where the bundled modules differed).
- **Do not use `**/build` here.** It also matches the Go source package
  `backend/internal/build`, which upstream added with the build/workspace
  feature this fork picked up in the 2.8.0 rebase. That stripped the package
  from the build context, so every image build — and therefore all three E2E
  jobs — failed with `internal/image/handler.go:16:2: no required module
  provides package .../internal/build`. The Go test and lint jobs stayed green
  because they build outside Docker, which is what hid it. Docker's ignore
  patterns are relative to the context root and are **not** recursive without
  `**`, so a bare `build` already means "only the context root's build/"; note
  that a leading slash (`/build`) is *not* the anchoring syntax Docker uses and
  silently matches nothing. Verified against `github.com/moby/patternmatcher`,
  the library Docker itself uses.
- **Re-apply notes:** If a future rebase brings more JS workspaces, add their
  `build/` paths here rather than reaching for a recursive pattern. Any
  recursive `**/<name>` pattern risks shadowing a Go package of the same name;
  `**/node_modules` and `**/.svelte-kit` are safe because no Go package can be
  called either.
- **Redundancy check:** Upstream added some recursive media/test patterns
  (`**/*.test.js`, `**/.DS_Store`, `**/*.mp4`), but the build-artifact entries
  `node_modules`, `build`, and `.svelte-kit` are still top-level only —
  **keep**. The `**/build` correction is fork-only debt and disappears with the
  entry.

### 4. Update contributor dev docs

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
  (`arcane-dev`) still match before re-applying. At the `b8bc5b4f` rebase
  upstream rewrote the same sections around Vite+ (`vp` as required
  prerequisite, `vp fmt`/`vp check` manual commands, a pre-commit-hooks
  section); the fork's Required/Optional prerequisites split and three-option
  Manual Commands structure were merged around it, with option 1 becoming
  "Vite+ / Justfile on the host".
- **Redundancy check:** Upstream `CONTRIBUTING.md` Prerequisites still list
  only Docker / VS Code / Vite+ with no optional host-tools guidance for the
  Justfile recipes, and its Manual Commands still omit the `-p arcane-dev`
  Compose project name — **keep**.

### 5. CI/workflows adapted for this fork

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
  action/toolchain pins in sync with upstream. At the `b8bc5b4f` rebase the
  fork `ci.yml` was re-derived onto upstream's Vite+ CI: the `type-check` and
  `e2e-tests` jobs now use `voidzero-dev/setup-vp@9446e853` (which replaces
  the separate pnpm/Node setup and the explicit `pnpm install`) and run
  Playwright via `vp -C tests exec`, and the Playwright browser cache key
  follows the root `pnpm-lock.yaml` (the frontend lockfile moved to a root
  pnpm workspace). The fork keeps its `push`/`main` triggers, `contents:
  read` permissions, the `github.ref` concurrency fallback (needed for push
  events), `ubuntu-latest` runners, `docker/setup-buildx-action` in
  `e2e-tests`, and the change-#12 mirror/retry image prefetch. Earlier pin
  history: at `73d13dc` the fork adopted node 26, `actions/checkout@v7`,
  `actions/setup-go@v7`, `golangci-lint-action@v9.3.0`, `actions/cache@v6.1.0`
  and upstream's `Lint protobuf definitions` step; at `c9fa64b` it inherited
  the loosened `golangci-lint-action@v9` / `actions/cache@v6` pins.
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

### 6. GitOps manual sync: honest feedback and per-row spinner

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
  (`frontend/messages/en.json`). Keep in step with the backend's sync
  coalescing, which returns `success=false` for an overlapping run. The spinner
  belongs in the status column, **not** in the `DropdownMenu.Item` (the menu
  closes on select, so a spinner there is never seen).
- **Redundancy check:** Upstream `sync-table.svelte` still toasts success on any
  2xx and uses the single `isLoading.syncing` flag with no status-column spinner
  — **keep**.

### 7. Shallow, tag-less GitOps clones and ls-remote connection test

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
  `Tags`) and `TestConnection` still clones-and-deletes — **keep**. (The
  `b8bc5b4f` acfs migration rewrote `BrowseTree`/`FileExists`/`ReadFile` in
  the same file but left `Clone`, `TestConnection` and
  `listRemoteReferences` untouched; the fork hunks replayed clean.)

### 8. Short commit hash display with full hash on hover

- **Files:** `frontend/src/lib/utils/navigation.ts`,
  `frontend/src/routes/(app)/environments/[id]/gitops/sync-table.svelte`,
  `frontend/src/routes/(app)/projects/[projectId]/+page.svelte`
- **What:** Add a `shortenGitCommit` helper (and `SHORT_GIT_COMMIT_LENGTH = 7`)
  beside `toGitCommitUrl`. Everywhere a GitOps commit hash is shown — the sync
  table `CommitCell` and the project header — render the abbreviated hash with
  the full hash in a `title` tooltip. The commit link's `href` still uses the
  full hash so it resolves. _(A third display site, the project page's
  read-only git-managed alert, was removed outright by upstream `6422c76` at
  the 2026-08-08 rebase; the fork's short-hash hunk there was dropped with
  it.)_
- **Why:** Full 40-character hashes are noisy in the UI; the short form reads
  better while the full value stays available on hover and in the link.
- **Re-apply notes:** The helper is appended to `navigation.ts` after
  `toGitCommitUrl`. Each display site derives `{@const shortCommit = ...}`,
  renders `shortCommit`, and adds `title={fullCommit}` (the sync table) or
  `title={project.lastSyncCommit}` (the project page). Keep every commit-link
  `href` on the full hash. Shares `sync-table.svelte` with change #6 — apply
  both when re-doing that file. Do **not** re-add the git-managed-alert site
  upstream removed; apply the treatment only where upstream itself renders a
  commit hash.
- **Redundancy check:** Upstream renders the raw full hash with no short form or
  `title` — **keep**.

### 9. Hide the copy button when the Clipboard API is unavailable

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
  in place, don't prune it. Upstream keeps reworking this component's markup
  (`5d6c07b2` added a `grid place-items-center` wrapper plus `col-start-1
  row-start-1` / `out:scale` on each icon branch), so re-apply by taking
  upstream's button body wholesale and re-deleting only the `{:else}` branch —
  don't force-keep the fork's copy of the button internals. Drop the
  `import * as Tooltip` line, which becomes unused.
- **Redundancy check:** Upstream `copy-button.svelte` still renders the
  disabled-button-with-tooltip fallback keyed on `isSecure` — **keep**. Note
  that upstream's `1a1d392a` fixed a *different* insecure-context bug (bulk
  actions calling the secure-context-only `crypto.randomUUID`); it does not
  touch the copy button.

### 10. GitOps commit-hash injection into the synced project's env

- **Files:** `backend/pkg/projects/env.go`, `backend/pkg/projects/env_test.go`,
  `backend/internal/models/gitops_sync.go`,
  `backend/internal/gitops/gitops_sync.go`,
  `backend/internal/gitops/service_test.go`,
  `backend/internal/gitops/service_unix_test.go`,
  `backend/resources/migrations/{sqlite,postgres}/073_add_gitops_sync_inject_commit_env.sql`,
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
  it (both dialects) to the next free number **and update change #11's
  constants and repair to cover the newly orphaned number**. History: `069` →
  `071` at the `c9fa64b` rebase (upstream passkeys took `069`/`070`), then
  `071` → `073` at the `b8bc5b4f` rebase (upstream volume-workspace/project
  tags took `071`/`072`) — each renumbering left lab databases with the old
  number recorded, which change #11 repairs at startup. The service code
  lives in `backend/internal/gitops/gitops_sync.go` since upstream's
  `1cea5f48` domain-package reorg. Injection has exactly two
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

### 11. One-time repair for databases migrated by a pre-renumber fork build

- **Files:** `backend/internal/database/database.go`,
  `backend/internal/database/database_test.go`
- **What:** Before running Goose upwards, detect a database that applied change
  #10's migration under one of its *old* numbers and repair it in place.
  `repairPreRenumberForkMigrationInternal` fires when
  `gitops_syncs.inject_commit_env` exists while version `73` is unrecorded. It
  applies everything below `73` through Goose first, then repairs whichever
  historical shape it finds:
  - **069-era** (fork builds `f3b8e1e`..`130b45f`): version 69 was recorded
    for the fork's migration, so upstream's `069` was skipped — the repair
    adds the missing `container_registries.repository_names` column.
  - **071-era** (fork builds `130b45f`..the 2026-08-15 rebase): version 71
    was recorded for the fork's migration, so upstream's `071`
    (volume-workspace legacy-key renames) was skipped — the repair replays
    its rename statements, which are idempotent by construction.

  Finally it records `73` as applied instead of re-running its DDL (the
  column already exists).
- **Why:** Goose keys its bookkeeping on the version number alone, so a
  database carrying the fork migration under an old number is broken in two
  ways on any later build: the fork migration's current number aborts with
  `duplicate column name: inject_commit_env` and Arcane refuses to start, and
  the upstream migration that now owns the recorded number is silently
  treated as applied and skipped.
- **Re-apply notes:** Purely fork debt from change #10's renumbering — nothing
  upstream will ever conflict with, though it sits in a file upstream does edit.
  The constants (`forkCommitEnvMigrationVersion` = 73,
  `forkCommitEnvMidRenumberVersion` = 71,
  `forkCommitEnvPreRenumberVersion` = 69) must be kept in step if a future
  rebase renumbers change #10's migration again: the historical numbers stay
  fixed (they are the facts being repaired), only the current number moves —
  and each new renumbering adds a new orphaned number whose upstream
  migration needs its own replay.
  `addSkippedRegistryRepositoryNamesColumnInternal` and
  `replaySkippedVolumeWorkspaceRenameInternal` duplicate the statements of
  the skipped upstream migrations; the tests compare a repaired database's
  schema against a from-scratch migration (and assert the replayed renames'
  data effects), so drift is caught rather than shipped. **Delete the whole
  thing** — repair, constants, the tests, and the README's closing sentence
  about it — once no pre-073 database is left running, which for a personal
  fork means once the lab instances have all been through one repaired
  startup.
- **Redundancy check:** Upstream cannot carry this; the state it repairs only
  exists because this fork renumbered its own migration — **keep** until the
  deletion criterion above is met.

### 12. E2E test images pulled from a mirror, with retries

- **Files:** `.github/workflows/ci.yml`, `.depot/workflows/ci.yml`,
  `tests/setup/project.data.ts`, `tests/spec/project.spec.ts`,
  `tests/spec/images.spec.ts`
- **What:** The nginx E2E test image comes from `mirror.gcr.io` (Google's
  anonymous Docker Hub pull-through cache) instead of `public.ecr.aws`, every
  prefetch pull (including the `ghcr.io` radarr image, which stays on ghcr)
  retries a failed pull three times with linear backoff, and the dead
  `docker save … > /tmp/test-images.tar` was dropped — nothing has ever read
  that tarball; the pull alone is what seeds the runner's image store. The
  image name is referenced in three places besides the workflow, so all of them
  move together or the prefetch stops matching what the fixtures ask for.
- **Why:** `public.ecr.aws` rate-limits anonymous pulls per source IP, and the
  three E2E matrix jobs pull the same image simultaneously from one runner, so
  `toomanyrequests: Rate exceeded` failed E2E jobs repeatedly before any test
  ran.
- **Re-apply notes:** Upstream-agnostic and worth upstreaming; both workflow
  files carry the identical step so they do not drift. Verify `mirror.gcr.io`
  still serves `library/nginx:stable-alpine`
  anonymously (manifest *and* blobs) before assuming a pull failure is
  transient. Note `tests/setup/compose*.yaml` still pulls `postgres:18-alpine`
  and `tecnativa/docker-socket-proxy:latest` straight from Docker Hub — the
  same class of exposure, not yet hit, and left alone.
- **Redundancy check:** Drop if upstream moves these pulls off `public.ecr.aws`
  itself.

### 13. Quieter startup: stop logging non-problems

- **Files:** `backend/pkg/projects/path_mapper.go`,
  `backend/pkg/projects/path_mapper_test.go`,
  `backend/pkg/projects/types/compose_content.go`,
  `backend/pkg/fswatch/watcher.go`, `backend/pkg/fswatch/watcher_test.go`,
  `backend/internal/apikey/service.go`,
  `backend/internal/role/service.go`,
  `backend/pkg/scheduler/scheduler.go`
- **What:** Six startup/sync log lines described conditions that were not
  happening; each is fixed at the source rather than by suppressing the log:
  - `PathMapper` gains `IsPathMounted` (also added to the
    `VolumeSourcePathMapper` interface in `compose_content.go`), because a
    matching bind mount (`-v /opt/docker:/opt/docker`) resolves a project
    directory to itself — indistinguishable, by comparing
    `ContainerToHost`'s output to its input, from a directory outside every
    mount. `hostWorkingDirInternal` now answers containment directly: a
    matching mount short-circuits silently (nothing to remap), and only a
    genuinely unmounted directory warns.
  - The projects filesystem watcher
    (`addExistingDirectoriesRecursiveInternal`) applies the same
    scratch/snapshot exclusions as the discovery walker
    (`IsInternalScratchDirName` / `IsFilesystemSnapshotDirName`), so Arcane's
    own GitOps staging and backup writes no longer wake the sync loop.
  - Unreadable directories (e.g. database data dirs owned by other users)
    drop from WARN to DEBUG in the watcher and stop the descent; other watch
    failures stay at WARN (that is what surfaces an exhausted inotify watch
    limit).
  - "Default admin user not found" (and "not a global admin") in
    `getDefaultAdminUser` drop to DEBUG; the actionable case — a configured
    static admin API key with no eligible account to attach to — gets its own
    WARN in `ReconcileDefaultAdminAPIKey`.
  - `upsertJobInternal` no longer logs "Job rescheduled" for a disabled job
    that was never scheduled.
  - `BackfillLegacyRoleAssignments` counts `RowsAffected` and only announces
    a backfill at INFO when it actually inserted assignments; the every-boot
    no-op drops to DEBUG.
- **Why:** A healthy lab install's startup logs were full of warnings about
  non-events, burying the messages that matter.
- **Re-apply notes:** Anchors: `hostWorkingDirInternal` /
  `isRemappableSourceInternal` in `path_mapper.go`,
  `addExistingDirectoriesRecursiveInternal` in `watcher.go`,
  `getDefaultAdminUser` / `ReconcileDefaultAdminAPIKey` in
  `internal/apikey/service.go`, `BackfillLegacyRoleAssignments` in
  `internal/role/service.go` (both moved out of `internal/services/` by
  upstream's `1cea5f48` domain-package reorg),
  and the `switch` in `upsertJobInternal`. The watcher exclusion depends on
  `projects.IsInternalScratchDirName` and
  `projects.IsFilesystemSnapshotDirName` still being exported. All of it is
  upstreamable; if submitting, note each piece stands alone.
- **Redundancy check:** Upstream still warns on every unmounted-looking
  project dir, still watches its own scratch directories, and still logs the
  admin-user and backfill lines at WARN/INFO unconditionally — **keep**.
  Drop any bullet upstream fixes independently.

### 14. Deploy falls back to build when a build-capable service's image can't be pulled

- **Files:** `backend/pkg/projects/pull_policy.go`,
  `backend/pkg/projects/pull_policy_test.go`
- **What:** `DecideDeployImageAction` now sets `FallbackBuildOnPullFail` for
  **every pull-eligible policy** (`missing`/empty, `always`,
  `daily`/`weekly`/`every_*`, and unknown values) when the service has a
  `build` section, instead of only when the effective policy string was empty.
  `never` (require local image) and `build` (build outright) are unchanged.
- **Why:** For a compose service with both `image:` and `build:`, `docker
  compose up` pulls and, if the pull fails, builds from source — the compose
  spec documents this and the embedded `docker/compose` library implements it
  for all pull-eligible policies (`pkg/compose/pull.go` swallows the pull error
  for services with `build` and queues them for the build stage). Arcane's
  deploy path had the matching fallback branch in
  `ensureDeployServiceImageReady` (`project_service.go`), but it was
  unreachable: `DeployProject` always resolves a non-empty deploy-level pull
  policy (the `defaultDeployPullPolicy` setting, ultimately `"missing"`), and
  `DecideDeployImageAction` substitutes that override when the service sets no
  `pull_policy` of its own — so the `policy == ""` branch, the only one that
  set the flag, never fired in a real deploy. Result: an unpullable image on a
  build-capable service aborted the deploy with "failed to pull image" instead
  of building, diverging from the docker compose convention.
- **Re-apply notes:** The change is one `switch` — the `buildEnabled` branch of
  `DecideDeployImageAction` — plus test cases in `TestDecideDeployImageAction`
  (the deploy-override case `DecideDeployImageAction(svc, "missing")` is the
  one that guards the actual bug). The flag's consumer is
  `ensureDeployServiceImageReady` in
  `backend/internal/project/project_lifecycle.go` (moved out of
  `internal/services/project_service.go` by upstream's `1cea5f48` reorg),
  which falls back only when
  `svc.Build != nil && decision.FallbackBuildOnPullFail` — confirm that
  consumer still exists after a rebase; if upstream restructures the deploy
  image preparation, re-apply the *intent*: any pull failure on a service with
  a `build` section (policy not `never`/`build`) must fall back to building,
  not abort the deploy. Upstreamable as a straight bug fix. Verify with
  `go test ./pkg/projects/...`.
- **Redundancy check:** Upstream's `DecideDeployImageAction` sets
  `FallbackBuildOnPullFail` only on the unreachable `policy == ""` branch —
  **keep**. Drop if upstream makes the fallback reachable itself (sets the
  flag for the `missing`/`always` branches, or rewires deploy so pull failures
  on build-capable services build instead of failing).

### 15. Convert a Git-synced project back into a regular project

- **Files:** `backend/internal/gitops/gitops_sync.go`,
  `backend/internal/gitops/handler.go`,
  `backend/internal/gitops/service_test.go`,
  `backend/internal/project/project.go`,
  `backend/internal/project/project_sync.go`,
  `backend/internal/project/service_test.go`,
  `backend/pkg/libarcane/edge/commands.go`,
  `frontend/src/lib/services/gitops-sync-service.ts`,
  `frontend/src/routes/(app)/projects/[projectId]/+page.svelte`,
  `frontend/messages/en.json`
- **What:** Two halves. (1) `POST
  /environments/{id}/gitops-syncs/{syncId}/detach` →
  `GitOpsSyncService.DetachManagedProjects`, which turns every project a sync
  manages back into a regular project: it clears `projects.gitops_managed_by`,
  clears the sync's `project_id`, switches `auto_sync` off and unregisters the
  recurring job, while leaving the project's files and containers and the
  sync's repository binding alone. The DB write is
  `ProjectService.ReleaseGitOpsProjectLinks`, the deliberate mirror of
  `EnsureGitOpsProjectLinked`, so the project domain keeps ownership of its own
  compose cache and events. Surfaced as a confirmed "Convert to regular
  project" button beside "Sync from Git" in the compose tab's read-only banner,
  requiring `gitops:update` **and** `projects:update`. (2)
  `SyncProjectsFromFileSystem` calls `clearOrphanedGitOpsLinksInternal`, one
  UPDATE releasing any project whose `gitops_managed_by` names a sync row that
  no longer exists.
- **Why:** `gitops_managed_by` is the only thing that marks a project as
  Git-managed, and nothing could clear it. Until upstream `775024e`
  (2026-08-12, first shipped in 2.8.0) deleting a sync did not clear it, so any
  database that deleted a sync on an older build carries projects that are
  permanently read-only — compose file, project name and workspace files all
  locked, `skipProjectCleanupInternal` exempting the row from filesystem
  cleanup, and no UI path to clear the flag because the sync it names is gone.
  Even on current builds, unmanaging a project meant deleting its sync and,
  since `DeleteRepository` refuses while any sync references it, unwinding the
  repository too.
- **Re-apply notes:** Two invariants are the whole point of the design, and
  both were found by review after a first attempt put the operation in the
  project domain:
  1. **Hold the sync's admission lease across the release.** `PerformSync`
     takes `s.jobs.TryAcquire(ctx, syncID)` and *then* loads the sync row, so a
     run already past admission holds the `ProjectID` it loaded. Without the
     lease, a detach can commit underneath it and the run will mirror
     repository files over the now-regular project and re-link it — either via
     `EnsureGitOpsProjectLinked` (whose guard passes once
     `gitops_managed_by` is nil) or via
     `updateProjectFromDirectorySyncInternal`, which writes
     `gitops_managed_by` unconditionally. Refusing with `common.ErrConflict`
     (→ 409) while a run is in flight is what makes the two mutually
     exclusive. This is why the operation lives in the GitOps domain: it is the
     side that owns the lease, and `internal/gitops` already imports
     `internal/project`, so the dependency cannot go the other way.
  2. **Unregister the job explicitly.** `runScheduledSyncInternal` returns
     early on `AutoSync=false` but does **not** unregister itself — only the
     `ErrNotFound` path does. (The doc comment above
     `registerSyncJobInternal` claims a row "toggled to AutoSync=false
     self-cancels"; it does not, and that comment is upstream's.) Setting the
     flag alone leaves a job firing and re-reading the row on every interval
     until restart, so the detach removes it and relies on `auto_sync=false`
     only for durability across restarts, since
     `RegisterAutoSyncJobsOnStartup` registers auto-sync rows only.
  A missing sync row needs no lease — nothing can run for it — and must still
  release its projects; that tolerance is what unsticks pre-2.8.0 databases,
  and it mirrors `DeleteSync`, which is likewise deletable when its row cannot
  be loaded. New project or sync routes also need a `commandRoutes` entry in
  `backend/pkg/libarcane/edge/commands.go` or edge-tunnel environments cannot
  reach them. No migration: the orphan reconcile is idempotent code on the
  project filesystem sync, which keeps this out of the fork's
  migration-renumbering debt. Upstreamable as a feature plus a recovery path
  for pre-2.8.0 databases. Verify with `go test ./internal/gitops/ -run
  DetachManagedProjects`, `go test ./internal/project/ -run
  'ReleaseGitOpsProjectLinks|ClearsOrphanedGitOpsLink'`, and `pnpm -C frontend
  check`.
- **Redundancy check:** Upstream has no detach/convert action and no orphan
  reconcile — **keep**. Drop the reconcile half if upstream adds its own
  cleanup for links to deleted syncs (a migration or a startup repair); drop
  the whole entry if upstream ships a way to unmanage a GitOps project.

---

## Superseded / now upstream

Changes the fork used to carry that upstream has since implemented
independently. Each entry names the upstream change that replaced it. Do
**not** re-introduce them:

- **Set background early to prevent white flash** *(was Active #2:
  `frontend/src/app.html`)* — **superseded by upstream `a6801855`** _(fix:
  prevent white flash on page load and refreshes)_, which injects its own
  bootstrap `<script>` into `app.html` doing what the fork's did — read the
  persisted `mode-watcher-mode` (default `system`), toggle the `dark` class
  on `<html>`, set `style.colorScheme` before hydration — plus restoring
  persisted appearance attributes/classes/CSS from `arcane-appearance`, which
  the fork's version never handled, and pairing it with a `layout.css`
  background rule. The fork's inline `<style>` block (explicit light/dark
  `html` backgrounds) was not re-added: upstream's `color-scheme` +
  `layout.css` approach covers the first paint. _Dropped at the 2026-08-15
  rebase onto `b8bc5b4f`; the fork commit was skipped during the replay._
- **Surface real rejection reason on tunnel register-send race** *(was Active
  #11: `backend/pkg/libarcane/edge/client.go`)* — **superseded by upstream
  `ccf4bd87`** _(chore(deps): bump google.golang.org/grpc from 1.82.1 to 1.83.0
  in /backend (#3481))_, whose `serveTunnelSessionInternal` now performs the
  same `io.EOF` fall-through the fork carried, so a stream the manager rejects
  before `Send` completes reports its real terminal status from `Recv` instead
  of a bare `"failed to send grpc tunnel register message: EOF"`. Upstream's
  gate is tighter than the fork's — `conn.Transport() != EdgeTransportGRPC ||
  !errors.Is(err, io.EOF)` restricts the fall-through to the gRPC transport,
  where the EOF-on-`Send` semantics actually apply, rather than the fork's
  transport-agnostic `!errors.Is(err, io.EOF)`. Nothing is lost by taking
  upstream's version: the websocket transport reports its close reason through
  `Recv` regardless. _Dropped at the 2026-08-05 rebase onto `60a8e663`._
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
