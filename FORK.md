# Fork changes

## Purpose of this fork

`jmclaren7/arcane` is a personal development fork of
[`getarcaneapp/arcane`](https://github.com/getarcaneapp/arcane), kept
deliberately thin and tracking upstream `main` closely. It exists to run
Arcane in a self-hosted lab from images the fork builds itself
(`ghcr.io/jmclaren7/arcane` and `ghcr.io/jmclaren7/arcane-agent`, tag `next`),
which upstream's release pipeline can't provide to a fork because it depends on
GoReleaser Pro, Depot runners, and cosign secrets. Everything the fork carries
falls into three buckets: **infrastructure adaptations** so CI and image builds
work outside upstream's environment (changes #3, #9); **documentation**
describing the fork itself and filling gaps in the contributor setup docs (#1,
#2); and a small set of **behavioural fixes, UI refinements and small
self-contained features** that are genuinely upstreamable but haven't been
submitted or merged yet (#4–#7, #10–#12) — plus one piece of fork-only debt, the
migration-renumbering startup repair (#8). One of those entries also carries
compatibility work that exists only because the fork changed shared behaviour
upstream later built on: change #12 keeps upstream's own auto-update status
readers aware of include mode. The fork deliberately carries no
divergent architecture and nothing that couldn't be offered upstream as-is —
every rebase resolves conflicts in
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
   helper renamed) replays cleanly and breaks only at compile time. This keeps
   happening: a dropped `fmt` import at `50c3d5d`, and at `41eae633` two
   separate frontend hunks (changes #9 and #15) still reading form state as
   Svelte stores (`$inputs.x`) after upstream had moved it to plain runes
   (`inputs.x`) — `git` reported a clean replay both times and only
   `svelte-check` caught them. A third class of silent breakage has no
   compiler to catch it at all: an upstream commit adding a **new reader** of
   a setting or helper the fork changed the meaning of (at `5feb10bf`,
   `57812f8f`'s `tagRegistryInternal.ExcludedContainers`, which read change
   #15's include-mode allowlist as a denylist). For every entry that
   reinterprets an existing field, re-grep the tree for its consumers rather
   than trusting a clean replay. The minimum gate is
   `go build -tags exclude_frontend ./...` and `go vet -tags exclude_frontend
   ./...` in `backend/` (Go 1.27+; upstream dropped the `GOEXPERIMENT=jsonv2`
   env at the go 1.27 upgrade — the build tag is what lets the backend build
   without a `frontend/dist` produced by a full frontend build), plus
   `pnpm install --frozen-lockfile && pnpm -C frontend run paraglide:compile
   && pnpm -C frontend check`. The paraglide compile is not optional: without
   it every `#lib/paraglide/messages.js` import fails to resolve and
   `svelte-check` reports hundreds of phantom errors. Since the `89619a824`
   rebase two more gates are mandatory, because neither the build nor
   `svelte-check` sees what they catch: **`pnpm -C frontend run lint`**
   (upstream's `shadcn/no-restyle` rule, added at `09e786e58`, rejects
   typography and color classes on `<Label>` — it flagged five fork labels that
   had been fine for months), and **the full `go test ./...`**, not just the
   packages an entry lists. Upstream's new test suites exercise fork-modified
   helpers from the outside: `internal/gitops/backup_test.go` broke on change
   #6's shallow clone through four layers of call stack, in a package the entry
   does not name. Run the Go suite against pristine upstream too before
   treating a failure as the fork's — the set that fails on upstream alone in a
   container with no Docker daemon moves from cycle to cycle, and at
   `919bd842e` it was `internal/gitops`'s
   `TestGitOpsBackup_DeletingProjectRemovesBackupSync` and
   `pkg/utils/notifications`'s two `TestBuildSignalURL` tests, neither of which
   the fork touches. The cheapest way to check is
   `git archive upstream/main | tar -x -C <tmpdir>` and run the same packages
   there. A fourth class joined at
   `a509628d4`, and only `go vet`/`go test` sees it: upstream's refactors
   **drop DI nil checks** from production code, so a fork _test_ that passed
   `nil` collaborators to a constructor becomes a SIGSEGV that aborts the whole
   package (`a432f2c` took out three of the detach entry's tests that way), and
   upstream **renames unexported test helpers** without touching anything the
   fork's production code compiles against
   (`newSettingsServiceForTestInternal` → `newSettingsServiceForTest` broke the
   include-mode entry's test). Build the **test** binaries, not just the packages:
   `go vet` is the cheapest gate that does.
   **Since the `94a2625b3` rebase** the gates changed shape: Paraglide is now
   generated by its Vite plugin (upstream `b5f849731`), so there is no
   `paraglide:compile` script any more — run `vp -C frontend run build` once
   (it writes `src/lib/paraglide`) before `vp -C frontend run check` and
   `vp -C frontend run lint`; `golangci-lint` must be **built with Go ≥ the
   module's `go` line** (`GOTOOLCHAIN=go1.27.4 go install
   github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`), or it
   refuses to load the config; run it in `backend/`, `cli/` and `types/`. In a
   cloud container that exports `GITHUB_TOKEN`, run `go test` with
   `env -u GITHUB_TOKEN` — upstream's `TestCheckLegacyEnvWarnsForRetiredNames`
   fails on that variable, not on the fork. Upstream's scheduler tests leave
   `actor-test-unused.francis.db` files in several `backend/internal/*`
   package directories; delete them, never `git add -A` them (one slipped into
   two fork commits during this rebase and had to be squashed back out).
4. Keep this file in sync: update the "Last rebased onto" marker, and move
   entries between the "Active" and "Superseded" sections as upstream evolves.
5. **Publish through the workflow, never by pushing `main` directly.** Once
   the result is verified and every conflict was resolved confidently, push
   the rebased tip to the `publish-to-main` branch
   (`git push --force origin HEAD:refs/heads/publish-to-main`). That runs
   `.github/workflows/publish-to-main.yml`, which keeps the old `main` as
   `backup/main-before-publish` and force-pushes the tip onto `main` with a
   lease. When a human decision is needed, push to `rebase-attempt` instead;
   the owner publishes it later with the workflow's manual run (source
   `rebase-attempt`) or by pushing that tip to `publish-to-main`.

> **Last rebased onto upstream:** `204ad2faa` (`204ad2faa80a78cc62689c399f1da46d31a7c23d`)
> — _fix: add missing bulk image pull and project destroy actions (#4914)_, on
> 2026-11-20.
> _(Previously `94a2625b3`, 2026-11-13. Earlier rebase narratives live in this
> file's history, e.g. `git show c91c6dd8f:FORK.md` for the `94a2625b3` one.)_
>
> This rebase carried **100 upstream commits** (releases 3.0.0 → 3.1.1).
> Themes: a new **`bidirectional` GitOps sync mode** (pull and push, with the
> sync table, form and project page all branching on it) and pulling Git backup
> changes into linked projects (`307e0d68d`, which added a `pull *PullRequest`
> parameter to `syncProjectDirectory`); Playwright E2E moved onto the
> **vite-plus test runner** (`26cefebba`: `vp -C tests test run`,
> `E2E_BASE_URL`, `playwright/package.json`, `test_projects` matrix column);
> new **Crowdin upload/download workflows** (`79c61a2b6`); LDAP login, project
> copy/move between environments, vulnerability-gated image updates, agent
> readiness leases, migrations `00114`–`00117`; plus dependency and Crowdin
> churn.
>
> **Outcome: all twelve entries stay active, none superseded; one new fork
> change (Crowdin gating, folded into #3).** Conflicts and resolutions:
>
> 1. **#3 `ci.yml`** — upstream's runner rewrite of the E2E job conflicted with
>    the fork's runner/prefetch hunks. Took upstream's new run step, matrix
>    rename and pins, kept `ubuntu-latest`, the change-#9 retrying prefetch (its
>    version probe now reads `playwright/package.json`, as upstream's does) and
>    the deletion of `cli-e2e-tests`. Upstream also dropped the
>    `if: github.head_ref != 'l10n_crowdin'` guard; the fork follows.
> 2. **#3 new: Crowdin workflows gated.** `crowdin-upload.yml` runs on a push to
>    `main` touching `frontend/messages/en.json` (fork changes do) and
>    `crowdin-download.yml` on a weekly schedule; both need depot runners and
>    upstream's Crowdin secrets. Each job now carries
>    `if: github.repository == 'getarcaneapp/arcane'` (upstream's own guard in
>    `release.yml`). Separate commit.
> 3. **#1 README** — upstream added a `> [!NOTE]` v3 notice at the top; the fork
>    banner goes first, then upstream's NOTE and IMPORTANT notices.
> 4. **#4 sync table** — upstream's bidirectional mode replaced
>    `canRunBackup`/`isBackup` gating with `canRunPush`/`pushesToGit` and added
>    `successMessage(sync, message)`. Re-applied `syncingIds` in both action rows
>    and the status column (spinner ahead of upstream's `pushesToGit` badge); the
>    success toast now uses `successMessage`, adding the server message as a
>    description except for bidirectional (whose title already is the message).
> 5. **#7 commit injection** — `syncProjectDirectory` now takes both upstream's
>    `pull` and the fork's `commitHash`. The new pull path (backup/bidirectional
>    pulls) passes `""`, so it never injects; the deploy `SyncDirectory` path
>    passes `source.commitHash` as before. The form's "Inject commit" switch is
>    hidden in bidirectional mode, because upstream's
>    `buildBidirectionalPayload` never submits it. **Flagged:** the backend
>    still accepts `injectCommitEnv: true` on a bidirectional sync (it only
>    rejects it on backup); it is stored but has no effect. Decide whether to
>    reject it in validation or support injection for bidirectional pulls.
> 6. **#9** — one nginx conflict in `tests/spec/projects.spec.ts` (upstream
>    reformatted the service-port spec); re-grep shows all six pull sites on the
>    mirror and `TEST_IMAGE_REFS.nginx` still deliberately on ECR.
> 7. **#11 detach** — project page: upstream replaced `isGitOpsManaged` with
>    `gitOwnsFiles`/`gitSyncsBothWays`; the fork's `canDetachFromGit` sits beside
>    them and the button stays in the `gitOwnsFiles` (deploy) banner only, so
>    bidirectional projects get no "Convert" button. **Flagged** for a decision
>    on whether detach should be offered for bidirectional syncs. Test files
>    were add/add conflicts at EOF; both sides kept.
> 8. **#12 auto-heal** — upstream added a hook/job-run label skip in
>    `filterCandidates`; kept it and swapped the fork's
>    `containerFilter.excludes` in for the map lookup. Re-greps clean: only
>    `container_filter.go` calls `ExcludedContainerNameSet` /
>    `ContainerNameExcluded`, `UpdaterService.ExcludedContainers` is still the
>    only `SettingsProvider` port, no new reader of either CSV.
> 9. **#8** — upstream's `00095` body unchanged; no new migration rebuilds
>    `gitops_syncs` (`00114`–`00117` checked).
> 10. #2, #5, #6, #10 replayed clean; their redundancy checks still hold
>     (upstream's copy button still renders the `isSecure` tooltip fallback,
>     its apikey lines still WARN).
>
> **Undocumented changes:** none — every file the previous fork diff touched is
> named by an entry.
>
> Verified post-rebase: `go build`/`go vet -tags exclude_frontend ./...`
> (backend) and `go build`/`go vet` (cli, types) clean; the full
> `go test -tags exclude_frontend ./...` in `backend/` passes (131 packages, 0
> failures, `GITHUB_TOKEN` unset), `go test ./...` passes in `cli` and `types`;
> `golangci-lint` 2.15.0 (built with go1.27.4) reports 0 issues in `backend`,
> `cli` and `types`, and `golangci-lint fmt --diff` is clean;
> `vp -C frontend run build`, `vp -C frontend run check` (0 errors, 0 warnings),
> `vp -C frontend run lint` and `vp fmt --check .` pass. E2E was not run (no
> Docker daemon in the rebase container). Tooling note: the container ships
> Node 22, so `vp` was installed with the official installer
> (`VP_VERSION=1.7.0 VP_NODE_MANAGER=yes`), which supplies Node 26.

---

## Active changes

### 1. "About this fork" README banner

- **Files:** `README.md`
- **What:** Prepend a blockquoted "About this fork" section above upstream's
  README that explains this is a dev fork, points at `FORK.md`, and
  lists the published image names/tags.
- **Why:** Makes the fork's purpose and image locations obvious to anyone who
  lands on the repo, and links to the change list.
- **Re-apply notes:** Insert the block above everything upstream's README
  opens with. Since upstream's `4ec4dc12` that is a `> [!IMPORTANT]`
  feature-requests notice rather than the `<div align="center">` this entry
  used to anchor on — the fork banner goes first, upstream's notice second,
  then the `<div>`. Keep the one-sentence summary of
  carried changes in sync with the Active list below (drop mentions of any
  change that moves to "Dropped"). At the `919bd842e` rebase it went from
  "four small self-contained features" to **three**: the
  build-on-pull-failure fallback went upstream, so its clause left the banner.

### 2. Update contributor dev docs

- **Files:** `CONTRIBUTING.md`
- **What:** Split Prerequisites into Required (Docker) and Optional (host
  tools for the Justfile shortcuts: `just`, `pnpm`/Node, Go 1.27+,
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
  At the `919bd842e` rebase upstream rewrote its own Prerequisites again:
  `a11074701` dropped the `gci`/`gofumpt` install block and added
  **`golangci-lint` v2 as a Required prerequisite**, because `golangci-lint
  fmt` now does the Go formatting the two tools used to. The fork's Optional
  list carried a `golangci-lint` bullet of its own, which that makes a
  duplicate of a Required one, so the bullet was **dropped**; the rest of the
  Optional list (`just`, `pnpm`, Go 1.27+) stands. Upstream also added an
  `/api/health/ready` curl to the troubleshooting section, taken as-is.
- **Toolchain line:** bumped to Go 1.27+ at the `1cb757731` rebase to match
  upstream's `84ce2ddf1`; still correct at `919bd842e` (`backend/go.mod` says
  `go 1.27.3` after upstream's `099161137`). Keep it in step with
  `backend/go.mod`.
- **Redundancy check:** Upstream `CONTRIBUTING.md` Prerequisites still list
  only Docker / VS Code / Vite+ / golangci-lint with no optional host-tools
  guidance for the Justfile recipes, and its Manual Commands still omit the
  `-p arcane-dev` Compose project name — **keep**.

### 3. CI/workflows adapted for this fork

- **Files:** `.github/workflows/ci.yml`, `.github/workflows/build-next-images.yml`,
  `.github/workflows/publish-to-main.yml`, `.github/workflows/crowdin-download.yml`,
  `.github/workflows/crowdin-upload.yml`
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
  `e2e-tests`, and the change-#9 mirror/retry image prefetch. At the
  `88514f7` rebase the fork `ci.yml` was re-derived again onto upstream's
  current file: it inherits the new `playwright_projects` matrix column (the
  sqlite job now runs the mobile/tablet/firefox/accessibility projects, the
  others chromium only) and drops upstream's new
  `actions/create-github-app-token` step along with the `deadcode` job it
  belongs to. Upstream also folded `depot configure-docker` into
  `depot/setup-action`'s `configure-docker: true` input; the fork has no depot
  steps to inherit that from. Earlier pin
  history: at `73d13dc` the fork adopted node 26, `actions/checkout@v7`,
  `actions/setup-go@v7`, `golangci-lint-action@v9.3.0`, `actions/cache@v6.1.0`
  and upstream's `Lint protobuf definitions` step; at `c9fa64b` it inherited
  the loosened `golangci-lint-action@v9` / `actions/cache@v6` pins.
  At the `89619a824` rebase upstream renamed `type-check` to `frontend-linter`
  (adding a `Run shadcn lint` step), SHA-pinned every action (`e0bb79670`), and
  gave the E2E job a `PLAYWRIGHT_DOCKER_IMAGE_REPOSITORY` pointing at its depot
  registry mirror plus a prefetch of the browser image (`af2e34f29`). The fork
  inherits the rename and the prefetch but drops the depot env, so
  `tests/setup/docker-browser.ts`'s public `mcr.microsoft.com/playwright`
  default applies; it also dropped the E2E job's `Setup Just` step, since
  nothing in that job runs `just` any more, and moved
  `docker/setup-buildx-action` to `v4` to match `build-next-images.yml`.
  At the `1cb757731` rebase the fork `ci.yml` was re-derived once more.
  Upstream **removed its own `deadcode` job** (so there is nothing left to
  drop there, and the `actions/create-github-app-token` step went with it),
  introduced a YAML anchor for the Go cache paths (`&go-cache-dependencies`
  defined in `go-tests`, referenced in `go-linter` — the fork drops
  `cli-e2e-tests`, which was its third user, so the anchor still resolves),
  moved the golangci config to the repo root (`../.golangci.yml`, was
  `../.github/.golangci.yml`), gave `go-linter` a Go module cache, added a
  `Setup Just` step and a svelte-check problem matcher to `frontend-linter`,
  dropped `--project=firefox` from the sqlite E2E matrix, folded `depot
  configure-docker` into `depot/setup-action`'s `configure-docker: true`
  input, and narrowed the Playwright report artifact to exclude
  `.report/auth`, `.report/bin` and `.report/cli`. The fork inherits all of
  that and still drops the depot runners, the three `depot/setup-action`
  steps, the `cli-e2e-tests` job and the `PLAYWRIGHT_DOCKER_IMAGE_REPOSITORY`
  depot-registry env, keeps its `push`/`main` trigger, `contents: read`
  permissions, the `github.ref` concurrency fallback, `ubuntu-latest`
  runners, `docker/setup-buildx-action` in `e2e-tests`, and the change-#9
  mirror/retry prefetch.
  At the `a509628d4` rebase upstream touched `ci.yml` only through `50afa2b`'s
  pin bumps, so the fork's file needed nothing but those two digests:
  `actions/upload-artifact` v7.0.1 → v7.0.2
  (`cf430e030ddbb5b0abf93d22962f4752f3646cd9`) and `voidzero-dev/setup-vp`
  v1.21.1 → v1.21.2 (`68f39c78aaf39be468cabd2afb09c162574c84af`). Note the
  fork has **two** of each, not three, because it drops the `cli-e2e-tests`
  job — grep both files for a stale digest after a bump rather than counting
  upstream's occurrences.
  At the `919bd842e` rebase upstream touched `ci.yml` only through
  `4056207af` _(chore: simplify e2e test jobs (#4522))_, which **removed the
  `proxy` entry from the `e2e-tests` database matrix** (the socket-proxy
  Compose file, port 3002) and pointed `cli-e2e-tests` at the ordinary
  `setup/compose.yaml` / port 3000 instead, renaming the job and its report
  artifact. The fork inherits the matrix removal — two E2E jobs now, sqlite
  and postgres — while the `cli-e2e-tests` half replayed as the usual
  **modify/delete conflict**, resolved by keeping the fork's deletion of the
  whole job. No action pin moved this cycle, so neither workflow file needed a
  digest bump.
  `build-next-images.yml` pins
  `actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1` and
  `docker/login-action@dbcb813823bdd20940b903addbd779551569679f # v4.6.0`,
  copied verbatim from upstream. The `docker/*` actions the fork uses alone
  (`setup-qemu-action`, `setup-buildx-action`, `metadata-action`,
  `build-push-action`) stay on tags: upstream pins none of them, so there is no
  verified digest to copy.
  The fork intentionally keeps the agent image named `arcane-agent` (not
  upstream's `arcane-headless`) because it reads better when the repo owner is
  not "Arcane"; preserve published image names so existing pullers don't break.
- **Redundancy check:** At `73d13dc` upstream **removed** its own
  `build-next-images.yml` (and `merge-conflict.yml`) and now publishes images
  only from `release.yml` via GoReleaser Pro, which a fork can't run. At the
  `5feb10bf` rebase upstream's `f0999495` re-created a next-image workflow,
  but under `.depot/workflows/build-next-images.yml`, built on GoReleaser Pro,
  depot runners and an `actions/create-github-app-token` step — still nothing
  a fork can run, and at a path the fork's file does not collide with. So the
  fork's `.github/workflows/build-next-images.yml` still has no upstream
  counterpart to re-derive from — it is a standalone fork workflow (it used to
  be a **modify/delete** conflict on rebase; resolve by keeping the fork
  file). Verify the Dockerfile paths it
  references still exist (`docker/Dockerfile`, `docker/Dockerfile-agent` — both
  present, both still take `VERSION`/`REVISION` build-args). Upstream's `ci.yml`
  still uses `depot-*` runners, Depot CLI, and the `deadcode` + `cli-e2e-tests`
  jobs. The fork adaptation is still required. **keep.**
- **Out of scope:** `release.yml` and the new `pr-quality.yml` are left at
  upstream — the fork has never customised them. Upstream deleted
  `build-pr-images.yml` entirely, so there is nothing left to leave alone.
- **At the `94a2625b3` rebase** `ci.yml` was re-derived from upstream's new
  file rather than hand-merged: upstream replaced `setup-go`'s built-in cache
  (and the `&go-cache-dependencies` anchor) with explicit `actions/cache`
  steps, switched `extractions/setup-just` to `taiki-e/install-action`, bumped
  `setup-vp` to v1.22.0 and dropped the `types` module from the Go matrices.
  The fork's edits on top are unchanged: `push` trigger, `contents: read`
  permissions, `ubuntu-latest` runners, no Depot CLI or depot env/permissions
  in `e2e-tests`, `docker/setup-buildx-action@v4`, the change-#9 pull-with-retry
  block, and no `cli-e2e-tests` job. `build-next-images.yml` replayed clean; its
  `docker/Dockerfile` and `docker/Dockerfile-agent` still exist.
- **At the `204ad2faa` rebase** `ci.yml` took upstream's vite-plus test
  runner (`vp -C tests test run`, `E2E_BASE_URL`, `test_projects`) and pins; the
  prefetch's version probe reads `playwright/package.json`. Upstream's new
  Crowdin workflows are gated per job with
  `if: github.repository == 'getarcaneapp/arcane'` (they need depot runners and
  upstream's Crowdin secrets; upload would fire on every fork `en.json` change).
  Keep that one-line guard rather than deleting the files, so upstream edits to
  them still replay. Drop the guard if the fork ever gets its own Crowdin project.
- **Publish workflow (added 2026-11-14, owner's request):**
  `publish-to-main.yml` force-pushes a branch onto `main`. It runs on a push to
  `publish-to-main` (the file comes from the pushed commit, so it works before
  it exists on `main`) or manually with a `source` branch input (default
  `rebase-attempt`). It refuses `main` as the source, saves the replaced tip
  as `backup/main-before-publish`, and uses `--force-with-lease`. It needs a
  **`MAIN_PUSH_TOKEN` secret**: a fine-grained PAT with Contents and Workflows
  read/write on this repository. `GITHUB_TOKEN` cannot push commits that touch
  workflow files, and its pushes would not trigger Build Next Images. Fork-only;
  upstream has no counterpart.

### 4. GitOps manual sync: honest feedback and per-row spinner

- **Files:** `frontend/src/routes/(app)/gitops/components/sync-table.svelte`
  (moved from `environments/[id]/gitops/` and made cross-environment by
  upstream at the `94a2625b3` rebase)
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
  — **keep**. At the `5feb10bf` rebase upstream's `dadf87f9` split the table on
  `isBackup(sync)`: `handlePerformSync` now takes the sync object, the status
  column renders a `BackupStateBadge` for backup rows, and there are two
  separate action rows. The fork's `syncingIds` set replaced `isLoading.syncing`
  in **both** rows (missing either leaves a dangling reference once the flag is
  gone from `$state`), the spinner branch sits ahead of the backup badge, and
  the toasts key on `result.success` while taking upstream's backup-vs-pull
  message pair.
- **At the `94a2625b3` rebase** the table moved to
  `routes/(app)/gitops/components/sync-table.svelte` and became
  cross-environment (`SyncRow`, `performSync(sync.environmentId, …)`,
  `invalidateSyncs()`); the fork's per-row `syncingIds` (keyed on the sync's
  UUID, unique across environments) and `result.success` toasts were re-applied
  onto it. **Flagged:** upstream's `PerformSync` now **waits** on a blocking
  admission gate (`entityjobs.Registry.Acquire`) instead of coalescing an
  overlapping run into `success=false`, so the premise in "Why" is weaker — the
  warning branch now fires only when a run reports `success=false` itself. The
  per-row spinner and the honest toast are still worth keeping.

- **At the `204ad2faa` rebase** upstream's bidirectional mode moved the action
  rows onto `canRunPush`/`pushesToGit` and toasts onto `successMessage(sync,
  message)`; `syncingIds` and the `result.success` branch were re-applied onto
  them.

### 5. Short commit hash display with full hash on hover

- **Files:** `frontend/src/lib/utils/navigation.ts`,
  `frontend/src/routes/(app)/gitops/components/sync-table.svelte`,
  `frontend/src/routes/(app)/projects/[projectId]/+page.svelte`
- **What:** Add a `shortenGitCommit` helper (and `SHORT_GIT_COMMIT_LENGTH = 7`)
  beside the repository-URL helpers (`toGitCommitUrl` until upstream's
  `6eed748e` generalized it into `toGitRouteUrl`). Everywhere a GitOps commit hash is shown — the sync
  table `CommitCell` and the project header — render the abbreviated hash with
  the full hash in a `title` tooltip. The commit link's `href` still uses the
  full hash so it resolves. _(A third display site, the project page's
  read-only git-managed alert, was removed outright by upstream `6422c76` at
  the 2026-08-08 rebase; the fork's short-hash hunk there was dropped with
  it.)_
- **Why:** Full 40-character hashes are noisy in the UI; the short form reads
  better while the full value stays available on hover and in the link.
- **Re-apply notes:** The helper is appended to `navigation.ts` after the
  repository-URL helpers. Each display site derives `{@const shortCommit = ...}`,
  renders `shortCommit`, and adds `title={fullCommit}` (the sync table) or
  `title={project.lastSyncCommit}` (the project page). Keep every commit-link
  `href` on the full hash, and take the URL from whatever helper upstream
  currently exposes — `toGitCommitUrl(url, hash)` became
  `toGitRouteUrl(url, 'commit', hash)` at the `41eae633` rebase, and the fork
  follows the rename rather than keeping a commit-only helper alive. Shares
  `sync-table.svelte` with change #4 — apply
  both when re-doing that file. Do **not** re-add the git-managed-alert site
  upstream removed; apply the treatment only where upstream itself renders a
  commit hash — `toGitRouteUrl`'s `tree`/`edit` callers (`gitops.ts`,
  `code-panel.svelte`) render paths, not hashes, and are left alone.
- **Redundancy check:** Upstream renders the raw full hash with no short form or
  `title` — **keep**.

### 6. Hide the copy button when the Clipboard API is unavailable

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
  that upstream's `1a1d392a` fixed a _different_ insecure-context bug (bulk
  actions calling the secure-context-only `crypto.randomUUID`); it does not
  touch the copy button.

### 7. GitOps commit-hash injection into the synced project's env

- **Files:** `backend/pkg/projects/env.go` (incl. `EnvContentChanged`),
  `backend/pkg/projects/env_test.go`, `backend/internal/project/model.go`
  (`GitOpsSync.InjectCommitEnv`), `backend/internal/project/service.go` (the
  `exhaustruct` copy in `EnsureGitOpsProjectLinked`),
  `backend/internal/gitops/service.go` (create/update/backup-mode validation),
  `backend/internal/gitops/children/sync/service.go` (the injection itself),
  `backend/internal/gitops/children/sync/service_test.go`,
  `backend/internal/database/fork_schema.go`,
  `backend/internal/database/fork_schema_test.go`, one hook line in
  `backend/internal/database/database.go`, `types/gitops.go`,
  `frontend/src/lib/types/gitops.ts`, `frontend/src/lib/types/ui/gitops.ts`
  (`GitOpsSyncWritable`), `frontend/src/lib/components/dialogs/gitops-sync-form.svelte`,
  `frontend/src/lib/components/dialogs/gitops-sync-form-fields.svelte`,
  `frontend/messages/en.json`
- **What:** An opt-in `injectCommitEnv` flag on a deploy-mode GitOps sync. When
  on, the sync appends `ARCANE_GIT_COMMIT`, `ARCANE_GIT_COMMIT_SHORT` and
  `ARCANE_GIT_BRANCH` to the Git-sourced env content it hands to the three-file
  env merge (`.env.git` + `project.env` → `.env`), so compose can interpolate
  them and `autoInjectEnv` can pass them to every service.
  `projects.BuildGitMetadataEnvContent` builds the block; the override builders
  skip the managed keys; `projects.EnvContentChanged` ignores a change in their
  _values_ (a commit that touches nothing synced does not redeploy) but not
  their appearance/disappearance (the toggle still reaches running
  containers). Single-file, directory and swarm-stack syncs are covered.
- **Why:** The synced commit was persisted (`gitops_syncs.last_sync_commit`) and
  shown in the UI, but nothing carried it into the container, so a deployed app
  could not report what it was deployed from.
- **Schema:** the `gitops_syncs.inject_commit_env` column is **not** a Goose
  migration any more. `ensureForkSchema` adds it idempotently right after
  `migrateDatabase` (SQLite: `pragma_table_info` check then `ALTER TABLE`;
  Postgres: `ADD COLUMN IF NOT EXISTS`). This ends the renumbering treadmill
  that produced nine repair eras: the fork never records a Goose version, so
  upstream can never collide with it. Trade-offs to know: an upstream SQLite
  migration that rebuilds `gitops_syncs` from an explicit column list would
  silently drop the column and its values (it is re-added empty on the next
  start — the opt-in resets to off); re-grep upstream's new
  `internal/database/migrations/*.go` for `gitops_syncs` rebuilds on every
  rebase. A downgrade (`AllowDowngrade`) leaves the column in place, which is
  harmless.
- **Re-apply notes:** Injection has exactly two call sites, both through
  `gitMetadataEnvContent`: the end of `prepareSyncSource` (single-file and
  swarm, which read `source.envContent`) and `syncProjectDirectory` right after
  upstream's own `sync.EnvContent` merge (directory sync; the commit hash is a
  parameter, ten upstream test call sites pass `""`). The block goes **after**
  upstream's per-sync env variables (`5512cc357`) so neither the repo's `.env`
  nor the sync's env can spoof the managed keys. The swarm branch writes the
  compose-dir `.env` when `sync.EnvContent != nil || sync.InjectCommitEnv` —
  upstream only writes it for its own env variables. Backup-mode syncs reject
  `injectCommitEnv: true` alongside upstream's other deploy-only options. In the
  form, add the field to the zod schema, the `syncToEdit` defaults and the
  submitted `fields`; `changedFields` picks it up through `GitOpsSyncWritable`.
  Copy the neighbouring switch row's markup and classes (upstream's
  `shadcn/no-restyle` lint rejects typography on `<Label>`).
- **Redundancy check:** Upstream's `5512cc357` added user-supplied per-sync env
  variables, not commit metadata; upstream still injects nothing about the
  commit — **keep**.

- **At the `204ad2faa` rebase** `syncProjectDirectory` gained upstream's
  `pull *PullRequest` parameter; the fork's `commitHash` follows it. The pull
  path (backup/bidirectional) passes `""` and never injects. The form hides the
  switch in bidirectional mode (upstream's bidirectional payload never sends it).
  **Open question:** backend validation still accepts the flag on a
  bidirectional sync, where it is stored but inert.

### 8. One-time repair for databases migrated by a fork build that recorded the fork migration as Goose `95`

- **Files:** `backend/internal/database/fork_migration_repair.go`,
  `backend/internal/database/fork_migration_repair_test.go`, one hook line in
  `backend/internal/database/database.go` (before `migrateDatabase`); it reuses
  `injectCommitEnvColumnExists` from change #7's `fork_schema.go`.
- **What:** Before Goose runs, if `goose_db_version` exists and
  `gitops_syncs.inject_commit_env` is present:
  - current version **== 95** — the database came from a fork build on the
    upstream 2.17.x line, which recorded change #7's migration as `95`.
    Upstream's v3 `00095_drop_retired_schedule_defaults` therefore counts as
    applied and would be skipped, so the repair replays its single statement
    (`DELETE FROM settings WHERE key IN ('dockerClientRefreshInterval',
    'autoHealInterval') AND value = '*/30 * * * * *'`, valid on both dialects)
    and leaves `95` recorded. Goose then applies `96…` normally; no upstream
    migration above `95` rebuilds `gitops_syncs`, so the column and the
    operator's opt-ins survive.
  - current version **< 95** — older fork bookkeeping (one of the nine historic
    renumbering eras). This build cannot repair it, because those repairs
    replayed SQL migration files upstream deleted. Startup fails with
    `errForkPreRenumberDatabase`, telling the operator to start the last v2
    fork build (`0950664`, also on `backup/main-pre-rebase-2026-11-07`) once,
    which carries the nine-era repair, then this build.
  - anything else (fresh DB, upstream DB without the column, a DB already past
    `95`) — no-op.
- **Why:** Goose keys bookkeeping on the version number alone; upstream reused
  `95` in its v3 baseline numbering.
- **Re-apply notes:** Purely fork debt. Because change #7 no longer records a
  Goose version, **no new era can ever be added** — this file only shrinks.
  Keep it out of `database.go` (one call line) so upstream's churn there does
  not conflict. If upstream ever changes `00095`'s body, update
  `forkReplayUpstream95SQL` to match (the test checks the replay's effect).
- **Delete the whole entry** (both files, the hook line, this section and the
  README's sentence about the repair) once every lab instance has started this
  build once.
- **Redundancy check:** Upstream cannot carry this — **keep** until the
  deletion criterion is met.
- **Previous design (removed at the `94a2625b3` rebase):** a ~1300-line,
  nine-era repair (fork migration recorded as 69, 71, 73, 74, 77, 85, 88, 90 or 94) that replayed the upstream SQL migrations each renumbering skipped. It
  lives on in `git show 09506642d:backend/internal/database/fork_migration_repair.go`.

### 9. E2E test images pulled from a mirror, with retries

- **Files:** `.github/workflows/ci.yml`, `tests/setup/project.data.ts`,
  `tests/setup/global-setup.ts`,
  `tests/setup/projects/test-project-static/compose.yaml`,
  `tests/setup/gitops-test-repo/compose.yaml`,
  `tests/spec/projects.spec.ts`, `tests/spec/images.spec.ts`
- **What:** The nginx E2E test image comes from `mirror.gcr.io` (Google's
  anonymous Docker Hub pull-through cache) instead of `public.ecr.aws`, every
  prefetch pull (including upstream's `public.ecr.aws/docker/library/busybox`
  — added by upstream's `081c9ac` ECR switch for the update-check specs —
  and `cgr.dev/chainguard/minio` / `ghcr.io/rustic-rs/rustic` for the S3-backup
  E2E, plus upstream's playwright browser image, all kept on their upstream
  registries)
  retries a failed pull three times with linear backoff, and the dead
  `docker save … > /tmp/test-images.tar` was dropped — nothing has ever read
  that tarball (upstream's `081c9ac` extended it with busybox, still unread);
  the pull alone is what seeds the runner's image store. The
  nginx image name is referenced in five places besides the workflow
  (`tests/setup/project.data.ts`,
  `tests/setup/projects/test-project-static/compose.yaml`,
  `tests/setup/gitops-test-repo/compose.yaml`, `tests/spec/projects.spec.ts`,
  and since the `89619a824` rebase the inspect-then-pull loop in
  `tests/setup/global-setup.ts`),
  so all of them
  move together or the prefetch stops matching what the fixtures ask for.
  The sixth fork-touched test file, `tests/spec/images.spec.ts`, is a
  different image: its "Pull Image" dialog spec types an image reference by
  hand and pulls it live, so its `public.ecr.aws/docker/library/alpine` was
  moved to `mirror.gcr.io/library/alpine` for the same reason. (Re-check that
  hunk whenever upstream edits the file — `41eae633` did, elsewhere in the
  same spec, without disturbing it.)
  The fifth nginx site arrived at the `89619a824` rebase: upstream's global
  setup gained an inspect-then-pull loop over busybox, alpine and nginx before
  bringing the stack up, and its ECR nginx entry is not satisfied by the
  workflow's mirrored prefetch, so every matrix job pulled nginx from ECR at
  setup anyway — for an image no fork fixture references any more.
  Two of the earlier four arrived at the `88514f7` rebase: upstream's `4a2f9ff4`
  E2E expansion added `tests/setup/projects/test-project-static/compose.yaml`
  and `tests/setup/gitops-test-repo/compose.yaml`, both pulling nginx from
  ECR at test time, and both were moved onto the mirrored name.
  The radarr prefetch was dropped at the `96729f7` rebase — upstream's
  `081c9ac` removed the image from the tests entirely. The new update-check
  specs (`image-updates.spec.ts`, `updates.spec.ts`) reference ECR image
  names for registry-metadata queries without pulling most of them; those
  names are upstream's choice of what to exercise and are left alone.
- **Why:** `public.ecr.aws` rate-limits anonymous pulls per source IP, and the
  three E2E matrix jobs pull the same image simultaneously from one runner, so
  `toomanyrequests: Rate exceeded` failed E2E jobs repeatedly before any test
  ran.
- **Re-apply notes:** Upstream-agnostic and worth upstreaming. **The fork no
  longer touches `.depot/workflows/ci.yml`.** Through the `5feb10bf` rebase the
  same step was mirrored into both workflow files so they could not drift; at
  `89619a824` upstream's `.depot` version (`af2e34f29`) started logging into its
  depot registry to pull the playwright browser image, which a fork has no token
  for and cannot mirror, so that file was resolved back to upstream and the
  mirrored retrying step now lives only in `.github/workflows/ci.yml`. Re-check
  on every rebase whether upstream's `.depot` step has become mirrorable again.
  Upstream's `.github/workflows/ci.yml` still prefetches from ECR and still
  writes the dead `docker save … > /tmp/test-images.tar`. Verify `mirror.gcr.io`
  still serves `library/nginx:stable-alpine`
  anonymously (manifest _and_ blobs) before assuming a pull failure is
  transient. On every rebase, re-grep the whole `tests/` tree for
  `nginx:stable-alpine` — new upstream fixtures are the usual way a pull
  drifts back onto ECR. Note `tests/setup/compose*.yaml` still pulls
  `postgres:18-alpine`
  and `tecnativa/docker-socket-proxy:latest` straight from Docker Hub, and
  upstream's newer specs pull `public.ecr.aws/docker/library/alpine:3.20` and
  `busybox:1.37` (`global-setup.ts`, `updates.spec.ts`,
  `docker-runtime-identity.spec.ts`) — the
  same class of exposure, not yet hit, and left alone. `image-updates.spec.ts`
  names ECR images too, but it queries their registry metadata without pulling
  them, so it is left alone as well.
  A **sixth** nginx site arrived at the `1cb757731` rebase: upstream renamed
  `tests/spec/project.spec.ts` → `tests/spec/projects.spec.ts` and its
  service-port spec deploys an nginx compose fixture of its own, so that one
  moved onto the mirror too. This is the third rebase in a row that added a
  site — the `grep -rn 'nginx:stable-alpine' tests/` is not optional.
  Verified at the `1cb757731` rebase that `mirror.gcr.io` still serves
  `library/nginx:stable-alpine` and `library/alpine:3.20` anonymously
  (manifest HTTP 200 for both).
  At the `a509628d4` rebase the re-grep turned up a **seventh**
  `nginx:stable-alpine` reference — `TEST_IMAGE_REFS.nginx` in the
  "Update checks" block of `tests/spec/images.spec.ts` — and it is
  **deliberately left on `public.ecr.aws`**. That block posts the name to
  `/api/environments/0/image-updates/check-batch`, which resolves registry
  metadata and never pulls, so it is the same class as the ECR names in
  `updates.spec.ts` and `image-updates.spec.ts`. Only a reference that causes
  a _pull_ belongs on the mirror; the grep is still mandatory, but the answer
  is not automatically "move it".
  At the `919bd842e` rebase the re-grep found **no new site** — the first
  rebase in four without one — and all six pull sites are still on the mirror,
  with `TEST_IMAGE_REFS.nginx` still deliberately on ECR. Upstream's
  `4056207af` removed the `proxy` E2E matrix entry, so the mirrored prefetch
  now seeds two jobs instead of three; the prefetch list itself is unchanged.
  Re-verified that `mirror.gcr.io` serves `library/nginx:stable-alpine` and
  `library/alpine:3.20` anonymously (manifest HTTP 200 for both).
- **Redundancy check:** Drop if upstream moves these pulls off `public.ecr.aws`
  itself. (Upstream's `081c9ac` moved nginx _within_ ECR, from
  `docker/library/nginx` to the `nginx/nginx` gallery namespace — still
  `public.ecr.aws`, still the same anonymous per-IP rate limits — **keep**.)

### 10. Quieter startup: stop logging non-problems

- **Files:** `backend/internal/apikey/service.go`
- **What:** "Default admin user not found" and "User is not a global admin" in
  `getDefaultAdminUser` log at DEBUG instead of WARN — on any install whose
  administrator does not use the default account they describe normal state.
  The actionable case, a configured static admin API key with no eligible
  account to attach to, gets its own WARN in `ReconcileDefaultAdminAPIKey`.
- **Why:** A healthy lab install's startup logs were full of warnings about
  non-events, burying the messages that matter.
- **Re-apply notes:** Upstream's `71712511f` made the admin username
  configurable (`getDefaultAdminUser(ctx, username)`), so log the configured
  `username`. **Do not return early** from `ReconcileDefaultAdminAPIKey` when the
  account is missing: upstream now runs its transaction anyway to delete
  managed keys still owned by a previous admin; the fork only adds the WARN.
- **Redundancy check:** Upstream still logs both lines at WARN unconditionally
  — **keep**. The watcher half of this entry went upstream at the `94a2625b3`
  rebase and its unreadable-directory half was dropped (see "Superseded / now
  upstream"); five of the entry's seven original bullets are now gone, so check
  each remaining one individually.

### 11. Convert a Git-synced project back into a regular project

- **Files:** `backend/internal/gitops/service.go`,
  `backend/internal/gitops/handler.go`,
  `backend/internal/gitops/module.go`,
  `backend/internal/gitops/service_test.go`,
  `backend/internal/project/service.go`,
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
     takes `s.jobs.TryAcquire(ctx, syncID)` and _then_ loads the sync row, so a
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
     missing-row path does. (Since the `c8ad3ed` rebase upstream's
     `UpdateSync` does unregister when auto-sync is toggled off through the
     API, but the detach writes `auto_sync=false` directly in SQL, so it
     still has to remove the job itself.) Setting the
     flag alone leaves a job firing and re-reading the row on every interval
     until restart, so the detach removes it and relies on `auto_sync=false`
     only for durability across restarts, since
     `RegisterAutoSyncJobsOnStartup` registers auto-sync rows only.
     At the `1cb757731` rebase upstream's `7d41c588a` standardized-domain-layout
     refactor deleted `internal/gitops/gitops_sync.go`, `internal/project/project.go`
     and `internal/project/project_sync.go`, so all three pieces were re-homed:
     `DetachManagedProjects` into `internal/gitops/service.go` (beside
     `PerformSync` and `DeleteSync`, which own the admission lease),
     `ReleaseGitOpsProjectLinks` and `clearOrphanedGitOpsLinksInternal` into
     `internal/project/service.go` (beside `EnsureGitOpsProjectLinked` and
     `SyncProjectsFromFileSystem`), and the route into `internal/gitops/module.go`,
     where upstream now registers them rather than in `handler.go`. The renamed
     collaborators are `getSyncByID` (was `getSyncRecordByIDInternal`),
     `s.jobs.Unregister` (was `unregisterSyncJobInternal`), `registerSyncJob`,
     `logProjectEvent`, `user.Actor` in place of `common.User`, and
     `Lease.Release(ctx)`.
     At the `a509628d4` rebase the production halves replayed untouched but
     **all three of this entry's tests had to be rebuilt**: `a432f2c` dropped the
     `if s.eventService == nil` guard from `logProjectEvent` and the nil guards
     from `KVService.ListByPrefix` and `projectPathMapper`, so
     `NewProjectService(db, settingsService, nil, …)` now panics. They construct
     the same collaborators upstream's own `SyncProjectsFromFileSystem` tests do
     (`event.NewEventService(db, config.Load(), nil)`,
     `image.NewImageService(db, nil, …)`, `testDockerService`,
     `testBuildBuilder{}`, `kv.NewKVService(db)`, `testRegistryCredentials`).
     Copy that construction from a neighbouring upstream test rather than passing
     `nil` for what an assertion does not need — every dropped guard upstream
     makes a `nil` here a package-wide SIGSEGV.
     A missing sync row needs no lease — nothing can run for it — and must still
     release its projects; that tolerance is what unsticks pre-2.8.0 databases,
     and it mirrors `DeleteSync`, which is likewise deletable when its row cannot
     be loaded. New project or sync routes also need a `commandRoutes` entry in
     `backend/pkg/libarcane/edge/commands.go` or edge-tunnel environments cannot
     reach them. The button belongs in whatever action row upstream's read-only
     Git banner currently renders, and takes that row's own conventions: at the
     `41eae633` rebase `6eed748e` added its own "Open project in repository" link
     there and dropped the row's `shrink-0` classes in favour of a `flex-wrap`
     container, so the detach button was re-applied as the last of the three
     without `shrink-0`. No migration: the orphan reconcile is idempotent code on the
     project filesystem sync, which keeps this out of the fork's
     migration-renumbering debt. Upstreamable as a feature plus a recovery path
     for pre-2.8.0 databases. Verify with `go test ./internal/gitops/ -run
     DetachManagedProjects`, `go test ./internal/project/ -run
     'ReleaseGitOpsProjectLinks|ClearsOrphanedGitOpsLink'`, and `pnpm -C frontend
     check`.
- **Redundancy check:** Upstream has no detach/convert action and no orphan
  reconcile — **keep**. Upstream moved adjacent at the `c8ad3ed` rebase
  without covering either half: `CleanupOrphanedSyncsOnStartup` releases and
  deletes syncs whose _environment_ is gone, and `DeleteSync` now clears
  `gitops_managed_by` even when the sync row can't be loaded — but nothing
  repairs a project whose `gitops_managed_by` names a sync that was _already
  deleted_ on an older build, and nothing converts a managed project back to
  a regular one. Drop the reconcile half if upstream adds its own
  cleanup for links to deleted syncs (a migration or a startup repair); drop
  the whole entry if upstream ships a way to unmanage a GitOps project.
- **At the `94a2625b3` rebase** (re-derived by hand; every backend file
  conflicted): the route is `POST /agents/{id}/gitops/syncs/{syncId}/detach`
  (upstream renamed `/environments/{id}/gitops-syncs` and turned the sync
  trigger into `POST …/{syncId}?action=sync`); the edge/agent command table moved
  to `backend/pkg/agent/commands.go` (`gitops_sync.detach`); handlers use
  `handlerutil.MessageOutput` and `types.Actor`; `NewProjectService` takes a
  `ProjectServiceParams` struct, so the project tests copy upstream's
  `SyncProjectsFromFileSystem` test construction. Upstream's entity-jobs
  registry no longer exposes `TryAcquire`, so **the detach now waits** for an
  in-flight run (`Acquire` with `defaultGitSyncTimeout`, exactly like
  `DeleteSync`) instead of refusing with 409; the lease is still held across
  the release, which is the invariant that matters, and
  `TestGitOpsSyncService_DetachManagedProjectsWaitsForActiveSync` replaces the
  old refusal test. `clearOrphanedGitOpsLinks` lost its `Internal` suffix with
  upstream's convention. Files now: `backend/internal/gitops/{service,handler,module}.go`,
  `backend/internal/gitops/service_test.go`, `backend/internal/project/service.go`,
  `backend/internal/project/service_test.go`, `backend/pkg/agent/commands.go`,
  `frontend/src/lib/services/gitops-sync-service.ts`,
  `frontend/src/routes/(app)/projects/[projectId]/+page.svelte`,
  `frontend/messages/en.json`.

- **At the `204ad2faa` rebase** the project page's `isGitOpsManaged` became
  upstream's `gitOwnsFiles`/`gitSyncsBothWays`; the button stays in the
  `gitOwnsFiles` banner, so bidirectional projects have no convert action.
  **Open question:** whether to offer detach for bidirectional syncs.

### 12. Include-mode switch for automation container lists

- **Files:** `backend/internal/settings/container_filter.go`,
  `backend/internal/settings/container_filter_test.go`,
  `backend/internal/settings/model.go`,
  `backend/internal/settings/service.go`,
  `backend/internal/settings/service_test.go`,
  `backend/internal/updater/service.go`,
  `backend/internal/container/service.go`,
  `backend/internal/dashboard/service.go`,
  `backend/pkg/scheduler/auto_heal_job.go`,
  `backend/pkg/scheduler/auto_heal_job_test.go`,
  `backend/internal/bootstrap/jobs_bootstrap.go`,
  `backend/internal/config/schema/schema.go`,
  `backend/internal/config/schema/schema_test.go`,
  `types/settings/settings.go`,
  `frontend/src/routes/(app)/environments/[id]/components/jobs-tab.svelte`,
  `frontend/src/routes/(app)/environments/[id]/components/environment-form-schema.ts`,
  `frontend/src/routes/(app)/environments/[id]/+page.svelte`,
  `frontend/src/lib/types/settings.ts`,
  `frontend/messages/en.json`
- **What:** Every environment automation that carries an "Excluded Containers"
  list — auto-update and auto-heal, on the environment Jobs tab — gets an
  "Include mode" switch (new boolean settings `autoUpdateIncludeMode` and
  `autoHealIncludeMode`, both defaulting to `false`) that inverts the list
  into "Include Containers": only the listed containers are affected by the
  automation and every other container is excluded. The names stay in the
  existing `autoUpdateExcludedContainers` / `autoHealExcludedContainers` CSVs;
  the mode only changes how the list is interpreted, so there is no migration
  and existing installs behave exactly as before. The Jobs-tab card flips its
  heading and description with the mode, and the resolved `autoUpdateEnabled`
  the backend reports for containers, container summaries and the dashboard
  follows it, so every surface that shows the ignored/enabled state does too.
- **Why:** In a lab where only a handful of containers should auto-update or
  auto-heal, an exclusion list has to be re-edited every time a container is
  added; include mode expresses "only these" directly as an allowlist.
- **Re-apply notes:** **The inversion lives in exactly one place:**
  `settings.ContainerAutoUpdateFilter`
  (`backend/internal/settings/container_filter.go`), reached through
  `SettingsService.ContainerAutoUpdateFilter`, which reads the list and the
  mode together so the two cannot drift apart at a call site. Add a consumer
  by calling that, never by parsing the CSV again.
  **It had to move at the `1cb757731` rebase.** Upstream's `636ba0950`
  relocated `backend/pkg/dockerutil` into the external
  `go.getarcane.app/docker` module (imported as `docker`), which a fork cannot
  extend, so the filter can no longer sit beside the
  `ExcludedContainerNameSet` / `ContainerNameExcluded` helpers it wraps. The
  settings domain is the right new owner: it owns the setting, every consumer
  already holds a settings service, and auto-heal keeps its own parse so no
  `pkg/` package needs to import `internal/`. If upstream moves those two
  helpers again, only this one file follows them.
  **At the `919bd842e` rebase the updater half moved onto upstream's new
  Docker boundary.** `fd742f3ee` _(refactor: rework the Docker boundary, image
  ownership, and other logic (#4484))_ replaced the updater service's direct
  `s.DockerClient(ctx)` calls with `s.deps.Docker.Call(ctx, func(ctx, *client.Client) (T, error))`,
  so `collectUsedImagesFromContainers` conflicted: upstream's side had the new
  `Call` wrapper and `docker.ExcludedContainerNameSet(…)` inline, the fork's
  had the old client plus `ContainerAutoUpdateFilter`. Resolution is upstream's
  boundary with the fork's filter — fetch `updateFilter` into a local, then
  take upstream's `Call`. `UpdaterService.ExcludedContainers` was moved onto
  `Docker.Call` in the same spirit even though `DockerClient` still exists
  (it is upstream's `updater.DockerClientProvider` implementation and only
  wraps `GetClient`); keep the port method itself, the engine needs it.
  **At the `a509628d4` rebase every backend consumer had to be re-derived onto
  upstream's composite literals.** `d2b4166` rebuilt `GetContainerDetails` and
  `BuildSummaries` around `containertypes.Details{ //exhaustruct:enforce }` /
  `Summary{ … }` literals that resolve `AutoUpdateEnabled` inline from
  `docker.ContainerNameExcluded(…, docker.ExcludedContainerNameSet(…))`, so the
  filter is now fetched into a local above each literal and the literal's own
  field reads `!autoUpdateFilter.Excludes(…)`. The same sweep **dropped DI nil
  checks** throughout, so the fork's `if s.settingsService != nil` /
  `if s.deps.Settings != nil` / `if s.deps.Docker == nil` guards went with
  them, and `UpdaterService.ExcludedContainers` reaches Docker through
  upstream's own `s.DockerClient(ctx)`. That is only safe because
  `SettingsService.ContainerAutoUpdateFilter` keeps its **nil-receiver guard**
  and returns a zero filter — do not "tidy" that away to match upstream's
  style; it is what makes every caller-side guard removable.
  The intent has one non-obvious constraint: the embedded
  updater engine (`go.getarcane.app/updater`) only understands an _exclusion_
  list through its `SettingsProvider.ExcludedContainers` port. In include mode
  Arcane's implementation of that port (`UpdaterService.ExcludedContainers`)
  materializes the inverse — the names of all Docker containers _not_ on the
  include list — so the engine's apply/restart paths honor the mode without a
  module change; drop the materialization if the engine ever grows a native
  include mode. Arcane-internal filtering uses the filter directly: both
  used-image collection paths in the updater service (direct containers and
  Compose-project containers, or "include none" is not a true no-op — upstream
  inlined the Compose one into `collectUsedImagesFromProjectsInternal` at the
  `1cb757731` rebase, so the guard lives in that loop now), the
  resolved `autoUpdateEnabled` on container details
  (`internal/container/service.go`), container summaries (`BuildSummaries`, same
  file) and the dashboard's fallback summaries
  (`internal/dashboard/service.go`) — all three added by upstream's `aaac10a0d`
  at the `89619a824` rebase — and `parseContainerFilterInternal` in the auto-heal
  job, which keeps its own parse because auto-heal has its own list and mode.
  **Any new upstream consumer of `autoUpdateExcludedContainers` has to be
  made mode-aware the same way, or include mode silently inverts for it** —
  and nothing in `git`, the build or the type-check catches it, so re-grep for
  `autoUpdateExcludedContainers`, for `ExcludedContainerNameSet` /
  `ContainerNameExcluded`, and for implementations of the updater
  engine's `SettingsProvider` port on every rebase. This is the entry's main
  maintenance cost and it has bitten at three rebases in a row. At
  `919bd842e` the re-greps came back clean: the only
  `ExcludedContainerNameSet` / `ContainerNameExcluded` callers are inside
  `internal/settings/container_filter.go`, `UpdaterService.ExcludedContainers`
  is still the only implementation of the port, and the one new
  `autoUpdateExcludedContainers` reference — in
  `internal/imageupdate/service_test.go` — is an upstream test asserting the
  opposite of a consumer ("the UI exclusion governs installation only; it must
  not reach the checker"), so there is nothing there to make mode-aware.
  `SetContainerAutoUpdateExclusionInternal` — which backs the per-container
  auto-update toggle and the Updates-page ignore action — inverts its
  add/remove in include mode so "enable auto-update" always means "make this
  container updatable" regardless of mode. Upstream's `6c227d02d` is orthogonal
  and needs nothing: a manual update passes
  `updater.Options{IgnoreSettingsExclusions: true}`, bypassing the list in both
  modes. Semantics to preserve: include mode with an
  empty list affects **no** containers (the list is the allowlist), and the
  `com.getarcaneapp.arcane.updater=false` label still overrides the list in
  both modes. A new setting key touches more places than the model struct:
  defaults in the settings service, the `Update` DTO in
  `types/settings/settings.go`, `overrideDocRules` and
  `expectedSettingOverrideKeys` in the config schema (+test), and — for
  auto-heal — the reschedule subscription key list in `jobs_bootstrap.go`.
  The Jobs-tab switch is now the entry's **whole** frontend half. Until the
  `89619a824` rebase the fork also computed the ignored state client-side
  (`isAutoUpdateIgnored` with an `includeMode` parameter, threaded into the
  Updates page, its container table and the container detail page); upstream's
  `aaac10a0d` moved that resolution onto the server, so all of it was dropped in
  favour of reading `container.autoUpdateEnabled` — see "Superseded / now
  upstream". Do not re-introduce it.
  **The Jobs-tab card gained an upstream neighbour at `919bd842e`.**
  `c8fda2bd4` _(feat: add auto-update delay for new images (#4459))_ inserted
  its own `auto-update-delay` block at the top of the `autoUpdateSettings`
  snippet, directly above the excluded-containers `<Label>` the fork wraps in
  a flex row with the include-mode `<Switch>`. That is a plain two-sided
  conflict: keep upstream's delay block first, then the fork's mode-aware row
  below it. The auto-heal half of the entry is untouched by this and merged
  cleanly.
  The Jobs-tab file was renamed `JobsTab.svelte` → `jobs-tab.svelte` at the
  `1cb757731` rebase, and at `a509628d4` its container list moved off a
  promise onto a TanStack query: the `{#await containersPromise then
  containers}` wrappers around both include-mode counters became
  `{#if containersQuery.isSuccess}` over a `containers` rune. A stale `#await`
  merges without a conflict and only `svelte-check` catches it, so take the
  surrounding block's data access verbatim, exactly as with `formInputs`.
  The Jobs-tab half reads `formInputs` in six places; copy the surrounding
  markup's access style **and its classes** rather than the fork's, because
  upstream keeps changing both — `5df09ed4`/`2d0ac4a8` moved the form off Svelte
  stores at the `41eae633` rebase, so the fork's
  `$formInputs.autoUpdateIncludeMode.value`
  reads replayed without a conflict and had to be rewritten to
  `formInputs.autoUpdateIncludeMode.value` (`svelte-check` catches that), and at
  `89619a824` upstream's new `shadcn/no-restyle` rule rejected the typography and
  color classes on all four include-mode `<Label>`s (only
  `pnpm -C frontend run lint` catches that).
  Upstreamable as a self-contained feature. Verify with `go test
  ./internal/settings/... ./internal/updater/... ./internal/container/...
  ./internal/dashboard/... ./pkg/dockerutil/... ./pkg/scheduler/...
  ./internal/config/...`, `pnpm -C frontend check` and
  `pnpm -C frontend run lint`.
- **Redundancy check:** Upstream's automation container lists are
  exclusion-only with no inversion switch — **keep**. Drop if upstream ships
  its own include/allowlist mode for these automations (watch for a rename of
  the `*ExcludedContainers` settings or a mode/select setting beside them), or
  if `go.getarcane.app/updater` grows a native include mode — then the
  materialized inverse can go with it. Two of the fork's inversions were dropped
  at the `89619a824` rebase because upstream stopped consulting the list in those
  paths at all; check each consumer individually rather than the entry as a whole.
- **At the `94a2625b3` rebase** the entry was re-homed: the auto-heal job is
  now `backend/internal/scheduler/auto_heal_job.go` (+ `_test.go`) with
  `autoHealContainerFilter` in the file's `type` block (`decorder`); setting
  defaults moved from `DefaultSettingsConfig` literals to `default:"false"`
  struct tags in `settings/model.go`; `GetStringSetting`/`GetBoolSetting` lost
  their default argument; `SetContainerAutoUpdateExclusionInternal` is now
  `SetContainerAutoUpdateExclusion`; the `types/settings` Update DTO is gone
  (no fork edit needed); upstream **deleted** `internal/dashboard/service.go`,
  so the dashboard fallback consumer disappeared with it; the kit module is
  `kit.getarcane.app/docker`. Upstream's new `ignoreOptOuts` context flag
  (manual updates bypass settings exclusions) gates the fork filter in both
  used-image collection paths. The Jobs tab became
  `frontend/src/lib/components/settings/environment/automations-tab.svelte`
  (two `SettingsRow layout="switch"` rows, one above each container list, with
  `lock`/`mixed` like their neighbours), plus the keys in
  `environment-form-schema.ts` and `routes/(app)/settings/automations/+page.svelte`.

---

## Superseded / now upstream

Changes the fork used to carry that upstream has since implemented
independently. Each entry names the upstream change that replaced it. Do
**not** re-introduce them:

- **Projects filesystem watcher skips Arcane's own scratch and snapshot
  directories** _(was part of Active #10: `backend/pkg/fswatch/watcher.go`)_ —
  **superseded by upstream `f6aad8689`** _(refactor: use
  kit.getarcane.app/tripwire for file system watcher)_, which deleted
  `pkg/fswatch` and configures the tripwire watcher with
  `SkipDir: projects.IsIgnoredProjectDirName` (= `IsInternalScratchDirName ||
  IsFilesystemSnapshotDirName`), the same exclusion the fork added. The entry's
  sibling bullet — demoting "unreadable directory" from WARN to DEBUG — could
  not be carried: that log now lives inside the external tripwire module and
  the entry's rule is to fix at the source, so it was **dropped, not
  superseded**; it needs an upstream tripwire change. _Both dropped at the
  2026-11-13 rebase onto `94a2625b3`._

- **Deploy falls back to build when a build-capable service's image can't be
  pulled** _(was Active #11: `backend/pkg/projects/pull_policy.go`,
  `pull_policy_test.go`)_ — **superseded by upstream `a78129075`** _(fix:
  stabilize container redeploys, exec sessions, and streams (#4488))_, which
  made the fallback reachable exactly the way the entry asked for: the
  `buildEnabled` branch of `DecideDeployImageAction` now sets
  `FallbackBuildOnPullFail` for `always`, for the refresh windows and for the
  `default`/`missing` case, and upstream **deleted** the two dead
  `case policy == "missing"` / `case policy == ""` arms the entry existed to
  work around. Upstream's own `TestDecideDeployImageAction` even gained the
  deploy-override case the entry called the one that guards the actual bug
  (`DecideDeployImageAction(svc, "missing")` → `PullIfMissing` **and**
  `FallbackBuildOnPullFail`), plus an `always` case and a negative case for a
  service with no `build` section, so the fork's test additions are redundant
  too. The consumer the entry named,
  `ensureDeployServiceImageReadyInternal` in
  `backend/pkg/projects/coordinator.go`, is unchanged. _Dropped at the
  2026-10-24 rebase onto `919bd842e`; the fork commit was skipped during the
  replay._
- **Exclude nested build artifacts from the Docker build context** _(was Active
  #2: `.dockerignore`)_ — **superseded by upstream `e0c20fe9c`** _(chore: cleanup
  docker context)_, which rewrote the file around recursive patterns: it now
  carries `**/node_modules`, `**/.pnpm-store` and `**/.svelte-kit`, which is what
  the fork entry existed to add. Upstream also solved the trap the entry
  documented at length — a bare `**/build` matching the Go source package
  `backend/internal/build` and breaking every image build — and solved it more
  directly than the fork did, with `!backend/internal/build` and
  `!backend/internal/build/**` negations rather than listing each JS workspace's
  `build/` path. The fork's `**/build` correction was pure fork-only debt and
  disappears with the entry. _Dropped at the 2026-10-10 rebase onto
  `1cb757731`; the fork commit was skipped during the replay._
- **Shallow, tag-less GitOps clones and ls-remote connection test** _(was Active
  #6: `backend/pkg/gitutil/git.go`, `git_test.go`, `git_write.go`)_ —
  **superseded by upstream's own `Clone` depth parameter**, which arrived with
  the acfs/gitrepo work in the 2.15 cycle. `Client.Clone` now takes
  `depth int` and the decision belongs to the caller, which is a better shape
  than the fork's unconditional `Depth: 1`: the sync path passes
  `kit.Ternary(pinned == "", 1, 0)` so replaying a pinned revision still gets
  full history, `internal/gitrepo` passes `1`, the Git **write** path
  (`CheckoutForWrite`, `checkoutNewBranchInternal`) passes `0` because go-git
  cannot commit and push from a shallow clone, and `internal/build` passes `0`.
  `TestConnection` is now a depth-1 clone rather than the full
  clone-and-delete the entry complained about, and upstream added `ProbeRemote`
  for ls-remote reachability. Crucially, upstream also carries **the fork's own
  compatibility work**: a clone refused for want of the `shallow` capability
  retries in full, with upstream's comment naming go-git's in-process test
  server as the case that needs it. The single residual, `Tags: git.NoTags`,
  was deliberately **not** re-added — on an already depth-1 single-branch clone
  it saves little, and setting it for every caller would risk the build and
  write paths that ask for full history, which is exactly the breakage class
  the old entry's own caveat warned about. _Dropped at the 2026-10-10 rebase
  onto `1cb757731`._
- **Quiet the "Job rescheduled" log for a never-scheduled disabled job** _(was
  one of change #12's bullets, latterly #10's: `upsertJobInternal` in
  `backend/pkg/scheduler/scheduler.go`)_ — **superseded by upstream
  `1cb757731`** _(refactor: migrate jobs to use fan out workflows (#4356))_,
  which replaced `upsertJobInternal` with `AddJob`/`RescheduleJob` and removed
  the reschedule logging entirely. There is no longer a line to suppress, so
  the bullet was dropped rather than ported and the entry no longer touches
  `scheduler.go`. Upstream's `e7317f026` _(fix(jobs): allow schedule updates
  for disabled jobs)_ reworked the same area in passing. _Dropped at the
  2026-10-10 rebase onto `1cb757731`._
- **Include-mode inversion in update discovery** _(was part of change #15:
  `backend/internal/imageupdate/image_update.go`,
  `backend/internal/imageupdate/service_test.go`)_ — **superseded by upstream
  `4eeb7d43d`** _(fix: keep checking updates for containers excluded from
  automatic updates (#4181))_. Upstream's `b9b2092` had taught
  `getAllImageRefsInternal` to drop images whose containers were on
  `autoUpdateExcludedContainers`, reading it as a denylist unconditionally, so
  the fork materialized the inverse set there in include mode. `4eeb7d43d`
  removed the setting from discovery entirely: only the update-check label
  (`imageref.IsUpdateCheckDisabled`) opts a container out of monitoring now, and
  a container excluded from automatic _installation_ keeps being checked. There
  is no longer a list to invert, so the fork commit was skipped during the
  replay and its test dropped with it. _Dropped at the 2026-09-26 rebase onto
  `89619a824`._
- **Include-mode inversion in the container tag-scan exclusion port** _(was part
  of change #15: `backend/internal/imageupdate/tag_updates.go`,
  `backend/internal/imageupdate/service_test.go`)_ — **superseded by upstream
  `4eeb7d43d`** as well. `57812f8f` had added
  `tagRegistryInternal.ExcludedContainers`, a second implementation of the
  updater engine's exclusion port feeding the tag scan straight from
  `autoUpdateExcludedContainers`, which the fork inverted from the Docker
  container list. `4eeb7d43d` deleted the method and stopped wiring a `Settings`
  provider into that engine at all, passing
  `updater.LabelPolicy{IsUpdateDisabledFunc: imageref.IsUpdateCheckDisabled}`
  instead — the read-only tag scan is deliberately not governed by the
  installation exclusions. _Dropped at the 2026-09-26 rebase onto `89619a824`._
- **Client-side auto-update ignored state** _(was part of change #15:
  `isAutoUpdateIgnored` in `frontend/src/lib/utils/container-auto-update.ts`,
  plus the `excludedContainers` / `autoUpdateIncludeMode` props threaded into
  `frontend/src/routes/(app)/updates/+page.svelte`,
  `container-updates-table.svelte` and
  `containers/[containerId]/+page.svelte`)_ — **superseded by upstream
  `aaac10a0d`** _(fix: resolve pending update status consistently across
  containers, projects, and dashboard (#4189))_ together with `4eeb7d43d`. The
  frontend used to recompute whether a container was ignored from the raw CSV
  and the mode; upstream now resolves it on the server and ships it as
  `autoUpdateEnabled` on container details, summaries and the dashboard, and the
  pages read that field. Making those three new backend readers mode-aware
  (change #15's re-apply notes) covers every surface at once, so the whole
  client-side path — the helper, its `includeMode` parameter and the prop
  threading — is gone. `parseExcludedContainerSet` and
  `isAutoUpdateLabelDisabled` stay: they are upstream's own and still used.
  _Dropped at the 2026-09-26 rebase onto `89619a824`._
- **PathMapper identity-mount warning** _(was one of change #12's bullets:
  `backend/pkg/projects/path_mapper.go`, `path_mapper_test.go`,
  `types/project/compose_content.go`)_ — **superseded by upstream
  `7b19b854`** _(fix: treat identity bind mounts as mapped when re-resolving
  escaped relative compose paths (#3924))_. A matching bind mount
  (`-v /opt/docker:/opt/docker`) resolves a project directory to itself, which
  upstream's `hostWorkingDirInternal` could not tell apart from a directory
  outside every mount, so it warned "project directory is not inside a mounted
  directory" for every project on every sync. The fork answered containment
  with a new `PathMapper.IsPathMounted` method (added to the
  `VolumeSourcePathMapper` interface); upstream instead changed
  `ContainerToHost` to return `(hostPath string, mapped bool, err error)` and
  gates the same warning on `!mapped`. Upstream's version is the better shape
  — the answer comes from the one lookup that already resolved the path,
  rather than a second traversal of the mount table — and it also added an
  identity-mount skip inside `RemapEscapedRelativeSources` that the fork's
  version never had. Take upstream's interface change wholesale; do not
  re-introduce `IsPathMounted`. _Dropped at the 2026-09-19 rebase onto
  `5feb10bf`._
- **Quiet the legacy `users.roles` backfill log** _(was one of change #12's
  bullets: `backend/internal/role/service.go`)_ — **superseded by upstream
  `a4ae1a0f`** _(fix: run legacy users.roles backfill once (#3964))_, whose
  `BackfillLegacyRoleAssignments` now accumulates `result.RowsAffected` into
  an `inserted` counter and emits its INFO line only when `inserted > 0` —
  the fork's bullet verbatim. Upstream went further and gated the whole
  backfill behind a `kv` marker (`migration.legacy_user_roles.v1.completed`)
  committed with the rows, plus a `NOT EXISTS` filter on the user query, so on
  a repeat boot it does no work at all rather than doing idempotent work
  quietly. The fork's extra DEBUG line for the no-op boot was not re-added:
  there is no longer a no-op boot to report. _Dropped at the 2026-09-19 rebase
  onto `5feb10bf`._
- **Preinstall Bun in the dev Dockerfile** _(was Active #2:
  `docker/Dockerfile.dev`)_ — **superseded by upstream `3dac10c2`** _(refactor:
  move from svelte-check-rs to official svelte-check)_ together with
  `3830802d` _(chore: use vite plus base docker image for frontend)_. The fork
  installed `ca-certificates`/`curl`/`unzip` plus Bun into the `frontend-dev`
  stage solely because `pnpm check` ran `svelte-check-rs`, which shells out to
  Bun and tried to auto-install it on a `node:*-slim` base that lacked the
  tools to do so. Upstream's `check` script is now
  `svelte-kit sync && svelte-check --tsgo ...`, which needs no Bun at all, and
  the `frontend-dev` stage now builds `FROM ghcr.io/voidzero-dev/vite-plus:latest`,
  which ships its own toolchain. Both halves of the fork change are moot;
  re-adding the `RUN` block would only slow the dev image build.
  _Dropped at the 2026-09-09 rebase onto `88514f7`; the fork commit was
  skipped during the replay._
- **Set background early to prevent white flash** _(was Active #2:
  `frontend/src/app.html`)_ — **superseded by upstream `a6801855`** _(fix:
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
- **Surface real rejection reason on tunnel register-send race** _(was Active
  #11: `backend/pkg/libarcane/edge/client.go`)_ — **superseded by upstream
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
- **Sanitize discovered project names** _(was Active #1:
  `project_service.go`, `project_service_test.go`)_ — **superseded by upstream
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
- **Keep LF line endings for scripts** _(was Active #5: `.gitattributes`)_ —
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
- **User avatar 404 when no email is set** _(was: `sidebar-user.svelte`)_ —
  **superseded by upstream `51977fc0`**, which extracted `sidebar-user.svelte`
  into its own component with an `if (!email) return ''` guard at the top of
  `getGravatarUrl`, so the Gravatar 404 no longer occurs and the fork's
  `&& user?.email` call-site guard is redundant.
  _Dropped at the 2.0.x rebase (2026-06-13)._
- **Toast on project form validation failure** _(was part of the project-name
  fix, `projects/[projectId]/+page.svelte`)_ — **superseded by upstream
  `8c750764`** _(fix: project save button not showing in tree view (#2163))_,
  whose `handleSaveChanges`
  now shows `toast.error(m.templates_validation_error())` via its own
  `hasAnyErrors` guard before the silent `if (!validated) return`, so the extra
  toast would be duplicative. _Dropped at the 2.0.x rebase (2026-06-13)._
