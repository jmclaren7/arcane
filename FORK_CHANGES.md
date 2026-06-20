# Fork changes (jcs-next)

This file is the authoritative list of changes that set this fork
(`jmclaren7/arcane`, branch `jcs-next`) apart from upstream
(`getarcaneapp/arcane`, branch `main`). It is written to be used by a human
or an AI agent when **rebasing onto a newer upstream or re-applying these
changes**.

When you rebase, work through every entry below. For each one:

1. Check whether upstream has since implemented an equivalent fix. If it has,
   **drop** the fork change and move it to "Dropped / now upstream" with a note.
2. Otherwise re-apply it, adapting to any code that moved. Re-run the listed
   verification.
3. Keep this file in sync: update the "Last rebased onto" marker, and move
   entries between the "Active" and "Dropped" sections as upstream evolves.

> **Last rebased onto upstream:** `20c608c2` — _feat: add project file tree
> management (#2893)_ (release 2.0.x), on 2026-06-13.

---

## Active changes

### 1. Sanitize discovered project names

- **Files:** `backend/internal/services/project_service.go`,
  `backend/internal/services/project_service_test.go`
- **What:** In `upsertProjectForDir`, set the new project's `Name` to
  `projects.SanitizeProjectName(dirName)` instead of the raw `dirName`, keeping
  the on-disk folder name in `DirName`/`Path`. Also self-heal existing rows on
  sync: if `SanitizeProjectName(existing.Name) != existing.Name`, update the
  `name` column and log it.
- **Why:** Discovered directories with characters outside `[A-Za-z0-9_-]`
  (e.g. `my.project`) produced a `Name` the editor's name field rejects, making
  the project silently uneditable from the UI.
- **Re-apply notes:** The patch sits in `upsertProjectForDir`. `SanitizeProjectName`
  lives in `backend/pkg/projects/fs_util.go` and is already imported in the
  service. Tests anchor after
  `TestProjectService_SyncProjectsFromFileSystem_RefreshesServiceCountOnComposeChange`
  and use helpers `setupProjectTestDB`, `createComposeProjectDir`, `ptr`, and
  `NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())`.
  Confirm the `NewProjectService` arity before re-applying.
- **Redundancy check:** Upstream still seeds `Name: dirName` unsanitised in the
  discovery path — **keep**.
- **Verify:** `go test ./internal/services/ -run 'TestProjectService_SyncProjectsFromFileSystem_(SanitizesDirNameWithDot|HealsExistingInvalidName)'`

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
  handling — **keep**.

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
  `frontend/package.json` and upstream's dev image still omits Bun — **keep**.

### 4. Exclude nested build artifacts from the Docker build context

- **Files:** `.dockerignore`
- **What:** Add recursive `**/node_modules`, `**/build`, `**/.svelte-kit`
  alongside the existing top-level entries.
- **Why:** Nested workspace folders were copied into the build context and
  caused build failures (e.g. on Windows, where the bundled modules differed).
- **Redundancy check:** Upstream's `.dockerignore` is still non-recursive —
  **keep**.

### 5. Keep LF line endings for scripts

- **Files:** `.gitattributes` (new file)
- **What:** Force `eol=lf` for `*.sh`, `*.bash`, and `Dockerfile`.
- **Why:** Git on Windows (`autocrlf`) rewrites them to CRLF, which breaks the
  scripts and Docker build.
- **Redundancy check:** Upstream has no `.gitattributes` — **keep**.

### 6. CI/workflows adapted for this fork

- **Files:** `.github/workflows/ci.yml`, `.github/workflows/build-next-images.yml`
- **Intent:** Make CI run on a fork without upstream-only
  infrastructure:
  - Run on the `jcs-next` branch (CI on push/PR; next images on push to
    `jcs-next`).
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
    names/aliases and the `linux/arm/v7` target.
- **Re-apply notes:** This is the highest-churn area on rebase — upstream
  frequently rewrites these workflows. Prefer re-deriving from upstream's
  **new** workflow files and re-applying the transformations above, rather than
  force-keeping the fork's stale copies, so upstream CI improvements are picked
  up. Keep action/toolchain pins in sync with upstream (currently node 26,
  `golangci-lint-action@v9.2.1`, `docker/login-action@v4.2.0`,
  `docker/build-push-action@v7`). The fork intentionally keeps the agent image
  named `arcane-agent` (not upstream's `arcane-headless`) because it reads
  better when the repo owner is not "Arcane"; preserve published image names so
  existing pullers don't break.
- **Redundancy check:** Upstream still relies on depot/GoReleaser/cosign — the
  fork adaptation is still required. **keep.**
- **Out of scope:** `build-pr-images.yml` and `release.yml` are left at
  upstream — the fork has never customised them.

---

## Dropped / now upstream

Changes the fork used to carry that upstream has since implemented (do **not**
re-introduce them):

- **User avatar 404 when no email is set** *(was: `sidebar-user.svelte`)* —
  upstream's `getGravatarUrl` now returns `''` early for a falsy email, so the
  Gravatar 404 no longer occurs. The fork's `&& user?.email` guard is redundant.
  _Dropped at the 2.0.x rebase (2026-06-13)._
- **Toast on project form validation failure** *(was part of the project-name
  fix, `projects/[projectId]/+page.svelte`)* — upstream's `handleSaveChanges`
  now shows `toast.error(m.templates_validation_error())` via its own
  `hasAnyErrors` guard before the silent `if (!validated) return`, so the extra
  toast would be duplicative. _Dropped at the 2.0.x rebase (2026-06-13)._
