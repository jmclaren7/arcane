> ## About this fork
>
> This is a development fork of [getarcaneapp/arcane](https://github.com/getarcaneapp/arcane). I develop directly on `main` here, periodically rebasing it onto upstream `main`. Container images are published to `ghcr.io/jmclaren7/arcane` and `ghcr.io/jmclaren7/arcane-agent` (tag `next`). The published images are for testing only.
>
> The full, up-to-date list of changes that set this fork apart from upstream — and the notes for re-applying them when rebasing — lives in **[FORK.md](FORK.md)**. In short, the fork carries expanded contributor docs, CI workflows adapted to run on a fork without upstream-only infrastructure (including E2E test images pulled from a mirror to dodge registry rate limits), a set of GitOps and UI refinements (honest manual-sync feedback with a per-row spinner, abbreviated commit hashes, a copy button that hides itself when the Clipboard API is unavailable, and quieter startup logging), and four small self-contained features: injecting the synced Git commit into a project's environment, converting a Git-synced project back into a regular one, falling back to building when a build-capable service's image can't be pulled, and an include-mode switch that turns the auto-update and auto-heal container lists into allowlists. It also carries one piece of fork-only debt: a one-time startup repair for lab databases whose migration bookkeeping the fork's own renumbering broke.
>
> Upstream README below.
>
> ` `

> [!IMPORTANT]
> Feature requests have moved to [Discussions](https://github.com/getarcaneapp/arcane/discussions/categories/feature-requests). Open new requests and upvote existing ones there; issues are only used for Bug reports now to help maintain priority on those issues.

<div align="center">

  <img src=".github/assets/img/arcane-full-trace-fill.svg" alt="Arcane Logo" width="500" />
  <p>Modern Docker Management, Designed for Everyone.</p>

<a title="Crowdin" target="_blank" href="https://crowdin.com/project/arcane-docker-management"><img src="https://badges.crowdin.net/arcane-docker-management/localized.svg"></a>
<a href="https://pkg.go.dev/github.com/getarcaneapp/arcane/backend/v2"><img src="https://pkg.go.dev/badge/github.com/getarcaneapp/arcane/backend.svg" alt="Go Reference"></a>
<a href="https://github.com/getarcaneapp/arcane/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-BSD--3--Clause-blue.svg" alt="License"></a>
<a href="https://snyk.io/test/github/getarcaneapp/arcane"><img src="https://snyk.io/test/github/getarcaneapp/arcane/badge.svg" alt="Known Vulnerabilities"></a>
[![Greptile: The War on Bugs](https://www.greptile.com/badge.svg)](https://www.greptile.com/?utm_source=oss_badge&utm_medium=readme&utm_campaign=greptile_for_open_source)

<br />

<img width="1685" alt="image" align="center" src=".github/assets/arcane-dash-2.18.1.png" />

## Documentation

For setup instructions, configuration details, and development guides, visit the **[official documentation site](https://getarcane.app)**.

## Sponsors

This project is supported by the following amazing people:

<p align="center">
  <a href="https://github.com/sponsors/kmendell">
    <img src='https://github.com/kmendell/static/blob/main/sponsors.svg?raw=true' alt="Logos" />
  </a>
</p>

## Security & Transparency

View the Software Bill of Materials (SBOM) for Arcane at **[getarcane.app/sbom](https://getarcane.app/sbom)**.

## Translating

Help translate Arcane on Crowdin: https://crowdin.com/project/arcane-docker-management

Thank you for checking out Arcane! Your feedback and contributions are always welcome.

</div>
