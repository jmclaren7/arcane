> ## About this fork
>
> This is a development fork of [getarcaneapp/arcane](https://github.com/getarcaneapp/arcane). I develop directly on `main` here, periodically rebasing it onto upstream `main`. Container images are published to `ghcr.io/jmclaren7/arcane` and `ghcr.io/jmclaren7/arcane-agent` (tag `next`). The published images are for testing only.
>
> The full, up-to-date list of changes that set this fork apart from upstream — and the notes for re-applying them when rebasing — lives in **[FORK.md](FORK.md)**. In short, the fork carries a flash-of-incorrect-theme fix, Docker build-context and dev-container fixes, expanded contributor docs, CI workflows adapted to run on a fork without upstream-only infrastructure, a set of GitOps/UI refinements (honest manual-sync feedback with a per-row spinner, faster shallow git clones, abbreviated commit hashes, and a copy button that hides itself when the Clipboard API is unavailable), and an edge-agent gRPC fix for a lost-rejection-reason race.
>
> Upstream README below.
>
> ` `

<div align="center">

  <img src=".github/assets/img/PNG-3.png" alt="Arcane Logo" width="500" />
  <p>Modern Docker Management, Designed for Everyone.</p>

<a title="Crowdin" target="_blank" href="https://crowdin.com/project/arcane-docker-management"><img src="https://badges.crowdin.net/arcane-docker-management/localized.svg"></a>
<a href="https://pkg.go.dev/github.com/getarcaneapp/arcane/backend"><img src="https://pkg.go.dev/badge/github.com/getarcaneapp/arcane/backend.svg" alt="Go Reference"></a>
<a href="https://github.com/getarcaneapp/arcane/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-BSD--3--Clause-blue.svg" alt="License"></a>
<a href="https://snyk.io/test/github/getarcaneapp/arcane"><img src="https://snyk.io/test/github/getarcaneapp/arcane/badge.svg" alt="Known Vulnerabilities"></a>

<br />

<img width="1685" alt="image" align="center" src=".github/assets/dashboard-1.19.3.png" />

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
