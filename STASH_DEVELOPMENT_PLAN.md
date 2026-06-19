# Stash Media Library Improvements Development Plan

## Ground Rules

- Do not build on the Synology NAS. Its role is to pull and run finished Docker images.
- Use `jethac/stash` as the fork and `jethac/media-library-improvements` as the feature branch.
- Keep changes sliced into reviewable commits.
- Prefer existing Stash patterns for GraphQL, SQLite stores, React components, and generated UI types.
- Run CPU-heavy checks locally or in GitHub Actions.

## Current Branch

- Fork: `https://github.com/jethac/stash`
- Branch: `jethac/media-library-improvements`
- Draft PR: `https://github.com/jethac/stash/pull/1`

## Phase 1: Movie And Performer Foundations

Status: mostly complete.

Tasks:

- Add performer `group_count` sorting and filtering support.
- Add movie/group `performer_count` filtering and sorting support.
- Add `/movies` route as a movie-focused entry point over groups.
- Rename or expose navigation so movies are discoverable.
- Render movie/group cards in poster aspect ratio.
- Show movie metadata on cards.
- Add missing-poster card state.

Validation:

- `go generate ./cmd/stash`
- `go test ./pkg/models`
- SQLite integration tests in Docker where Windows CGO/sqlite blocks local execution.
- `pnpm run check`
- `pnpm run lint:js`
- `pnpm run build`

## Phase 2: Localized Title Data Model And API

Status: mostly complete.

Tasks:

- Add `localized_titles` SQLite table and migration.
- Add model and repository interfaces.
- Add SQLite store with create, update, upsert, destroy, and lookup methods.
- Add GraphQL types, inputs, queries, and mutations.
- Add `localized_titles` fields to group/movie and scene GraphQL types.
- Generate backend and frontend GraphQL artifacts.
- Add focused store tests and API compile/test coverage.

Validation:

- `go test ./pkg/models/...`
- Docker API test: `go test ./internal/api -count=1`
- Docker SQLite focused integration test for localized titles.
- `pnpm run gqlgen`
- `pnpm run check`

## Phase 3: Localized Title UI

Status: complete.

Completed:

- Add localized title data to movie/group list queries.
- Add movie list title-language selector.
- Add localized title fallback helper.
- Add movie/group edit fields for English, Japanese, and French localized titles.
- Add movie/group detail title-language selector.
- Add localized title search/filter support.
- Add a missing-localized-title filter for cleanup.
- Keep title language selection per-view for this build rather than adding a global setting.
- Add localized title display to the compressed movie/group detail header.
- Defer scene localized-title editing until the movie/group workflow is proven.

Remaining tasks:

- None.

Validation:

- `pnpm run gqlgen` after GraphQL document changes.
- `pnpm run check`
- `pnpm run lint:js`
- `pnpm exec stylelint src/components/Groups/styles.scss`
- `pnpm run build`

## Phase 4: Poster And Movie Cleanup Workflows

Status: complete.

Completed:

- Add movie cleanup shortcuts for missing poster, missing localized title, missing performers, and missing studio.
- Add scene cleanup shortcuts for ungrouped scenes, missing performers, and missing studio.
- Audit existing group front/back image upload and URL flows.
- Keep the existing front-image-backed poster storage, and label the group front-image edit action as poster management.
- Verify `is_missing: "poster"` is already backed by the group front-image blob filter.
- Keep cleanup shortcuts read-only; they only apply filters and do not perform bulk writes.

Remaining tasks:

- None.

Validation:

- Backend filter tests for any new filter operations.
- UI tests or focused manual checks for cleanup filters.
- Production UI build.

## Phase 5: Metadata Import Helpers

Status: in progress.

Completed:

- Define CSV/JSON import format for localized title backfills.
- Add `scripts/localized_titles_backfill`, a workstation-run GraphQL helper for localized title imports.
- Add dry-run mode, duplicate detection, and conflict reporting.
- Require `--apply` for writes and `--overwrite` before replacing existing different values.
- Add `scripts/group_import`, a workstation-run GraphQL helper for creating Stash groups as movie records from CSV/JSON exports.
- Support group import dry-runs, duplicate detection, exact-name existing group reuse, poster/front-image URLs, and localized title upserts.
- Dry-run `scripts/group_import` against production Stash with a throwaway movie row and verify it makes no writes.

Remaining tasks:

- Optionally support Plex database derived title exports where source data is reliable.
- Run a dry-run against production Stash with a real movie/group export.
- Run a dry-run against production Stash with a real localized-title export.
- Back up production Stash config/database before any apply run.
- Apply a verified movie/group import so `/movies` contains real poster-driven records.

Validation:

- `go test ./scripts/localized_titles_backfill`
- `go test ./scripts/group_import`
- Dry-run against a copied database or fixture.
- Upsert test with duplicate language/object pairs.
- Backup and restore drill before running on production data.

## Phase 6: Docker Image And Synology Deployment

Status: in progress.

Tasks:

- Add or reuse GitHub Actions workflow for Synology amd64 Docker image builds. (Complete)
- Publish to GitHub Container Registry under `ghcr.io/jethac/stash`. (Complete)
- Tag images by branch SHA and optional semantic label. (Complete)
- On Synology, back up the Stash config directory and database. (Complete for current deployment)
- Pull the finished image on Synology. (Complete for current deployment)
- Update the Stash container image reference. (Complete for current deployment)
- Restart the container. (Complete for current deployment)
- Smoke test login, movie list, movie detail, localized title edit, and scene playback.

Synology rule:

- Never compile Go, run frontend builds, or run Docker image builds on the NAS.
- Use Synology only for `docker pull`, container restart, and runtime smoke tests.

## Recommended Next Work

1. Produce a real movie/group CSV or JSON export from Plex or another reliable source.
2. Dry-run `scripts/group_import` against production Stash and inspect conflicts.
3. Back up production Stash config/database, then apply the verified group import.
4. Dry-run and apply `scripts/localized_titles_backfill` for any additional localized titles not included in the group import.
5. Smoke test `/movies` with real groups, poster images, localized title switching/editing, performer incidence sorting, and scene playback.
