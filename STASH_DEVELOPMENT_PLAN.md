# Stash Media Library Improvements Development Plan

## Ground Rules

- Do not build on the Synology NAS. Its role is to pull and run finished Docker images.
- Use `jethac/stash` as the fork and `jethac/media-library-improvements` as the feature branch.
- Keep changes sliced into reviewable commits.
- Prefer existing Stash patterns for GraphQL, SQLite stores, React components, and generated UI types.
- Run CPU-heavy checks locally, in GitHub Actions, or on the Ubicloud `hijinks-build-x64` amd64 build VM.

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

Status: complete, with follow-up movie-list refinements in progress.

Completed:

- Add movie cleanup shortcuts for missing poster, missing localized title, missing performers, and missing studio.
- Add scene cleanup shortcuts for ungrouped scenes, missing performers, and missing studio.
- Audit existing group front/back image upload and URL flows.
- Keep the existing front-image-backed poster storage, and label the group front-image edit action as poster management.
- Verify `is_missing: "poster"` is already backed by the group front-image blob filter.
- Keep cleanup shortcuts read-only; they only apply filters and do not perform bulk writes.
- Make `/movies` render movie cards with cover-style poster image fitting while keeping `/groups` unchanged.
- Add folder/directory filtering to the movie/group sidebar, backed by related scene-file folder joins in the group/movie filter.

Remaining tasks:

- None.

Validation:

- Backend filter tests for any new filter operations.
- UI tests or focused manual checks for cleanup filters.
- Production UI build.

## Phase 5: Metadata Import Helpers

Status: complete.

Completed:

- Define CSV/JSON import format for localized title backfills.
- Add `scripts/localized_titles_backfill`, a workstation-run GraphQL helper for localized title imports.
- Add dry-run mode, duplicate detection, and conflict reporting.
- Require `--apply` for writes and `--overwrite` before replacing existing different values.
- Add `scripts/group_import`, a workstation-run GraphQL helper for creating Stash groups as movie records from CSV/JSON exports.
- Support group import dry-runs, duplicate detection, exact-name existing group reuse, poster/front-image URLs, and localized title upserts.
- Dry-run `scripts/group_import` against production Stash with a throwaway movie row and verify it makes no writes.
- Support optional `scene_path` matching and group-to-scene linking for imported movie records.
- Support Plex database derived movie/group exports where source data is reliable.
- Generate a real Plex-derived Ero movie/group export with poster/front-image data URLs and path rewriting from `/medialibrary/Ero` to `/data`.
- Dry-run the real Plex-derived export against production Stash: 823 groups, 1,012 localized titles, 889 scene links, zero conflicts, zero missing scenes, and zero writes.
- Back up production Stash config/database before the apply run to `/volume1/docker/backups/stash-20260619-164135-pre-group-import/config`.
- Apply the verified Plex-derived group import to production Stash: 823 groups, 1,012 localized titles, and 889 planned scene links.
- Fill the remaining 411 groups without Plex poster art from linked-scene screenshots, leaving zero groups missing front images.
- Repair the one multi-file-scene import edge case where a later scene update replaced an earlier group link; final production state is 823 groups, 1,012 localized group titles, 889 group-scene links across 888 distinct scenes, and zero unlinked groups.
- Update `scripts/group_import` so scene link updates preserve existing scene groups when adding a new one.

Validation:

- `go test ./scripts/localized_titles_backfill`
- `go test ./scripts/group_import`
- Dry-run against production before apply with zero conflicts, zero missing scenes, and zero writes.
- Upsert test with duplicate language/object pairs.
- Production backup verified before import. A restore drill was not executed.

## Phase 6: Docker Image And Synology Deployment

Status: in progress.

Tasks:

- Add or reuse GitHub Actions workflow for Synology amd64 Docker image builds. (Complete)
- Use the Ubicloud `hijinks-build-x64` VM for manual amd64 Docker builds when GitHub Actions is not the right place to wait. (Complete)
- Publish to GitHub Container Registry under `ghcr.io/jethac/stash`. (Complete)
- Tag images by branch SHA and optional semantic label. (Complete)
- On Synology, back up the Stash config directory and database. (Complete for current deployment)
- Pull the finished image on Synology. (Complete for current deployment)
- Update the Stash container image reference. (Complete for current deployment)
- Restart the container. (Complete for current deployment)
- Smoke test HTTP access to `/`, `/movies`, and `/scenes`. (Complete)
- Smoke test production GraphQL counts for movies, localized titles, posters, multi-scene groups, and performer incidence sorting. (Complete)
- Smoke test rendered login, movie list, movie detail, localized title edit, and scene playback in a browser.

Synology rule:

- Never compile Go, run frontend builds, or run Docker image builds on the NAS.
- Use Synology only for `docker pull`, container restart, database backup/verification, and runtime smoke tests.
- Build amd64 images on GitHub Actions or a real build host such as the Ubicloud `hijinks-build-x64` VM, not on the Synology CPU.

## Recommended Next Work

1. Run rendered browser smoke tests for `/movies`, movie detail, localized title switching/editing, performer incidence sorting, and scene playback.
2. Decide whether to deploy the latest scripts-only image build; no Synology runtime redeploy is required for the completed metadata import.
3. Deploy the next custom image to Synology only after the Ubicloud/GHCR build is complete; do not build on the NAS.
