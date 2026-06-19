# Stash Media Library Improvements PRD

## Objective

Fork Stash under `jethac`, implement movie-focused library improvements, and deploy a custom build to the Synology NAS as a Docker image. The Synology is a runtime target only; builds and tests must run on a workstation, CI, or another stronger build host.

## Problems

Stash is strong for scene-level browsing, but it is weak for a Plex-like movie library:

- Movies/groups do not feel like first-class poster-driven library objects.
- Movie title handling is not good enough for libraries with English, Japanese, French, and native titles.
- Performer browsing cannot be sorted by how often performers appear in the local movie library.
- Posters, localized titles, and attribution cleanup require too much manual work outside the main movie workflow.

## Goals

1. Add a movie-first browsing experience with poster cover art, movie metadata, and clear movie navigation.
2. Preserve scene-level Stash behavior while making movies/groups usable as a primary library view.
3. Support localized titles so a user can switch between native, English, Japanese, and French display titles.
4. Let performer lists be sorted by incidence in the local library, especially movie/group incidence.
5. Add practical editing workflows for localized movie titles and poster metadata.
6. Build and publish a custom Docker image for deployment on Synology.

## Non-Goals

- Replace Stash's scene data model.
- Implement a full Plex clone.
- Build release artifacts on the Synology NAS.
- Require external metadata providers for basic title and poster editing.
- Force one title language globally before per-page controls are useful.

## Users

Primary user: one technical administrator with a private media library, comfortable with Docker, GitHub, and NAS administration.

Secondary user: future self using the library repeatedly from the Stash web UI, needing fast browsing and low-friction metadata cleanup.

## Core Requirements

### Movie Library

- Provide a `/movies` route that uses Stash groups as movie records.
- Show movies as poster-oriented cards.
- Prefer front cover/poster art over scene thumbnails.
- Display useful movie metadata on cards, including date/year, duration, studio, scene count, performer count, and subgroup count where available.
- Keep existing group URLs working.

### Movie Detail

- Movie detail pages must support poster-style presentation.
- Movie detail title display must support localized title switching.
- Editing movie metadata must stay compatible with existing group create/update mutations.

### Localized Titles

- Store localized titles separately from canonical names.
- Support localized titles for groups/movies first, with an extensible object model for other entity types.
- At minimum, support native, English (`en`), Japanese (`ja`), and French (`fr`) display choices.
- Provide GraphQL query and mutation APIs for localized title create, update, upsert, destroy, and lookup.
- UI must fall back to the canonical name when a selected language is missing.

### Performer Incidence

- Performer list sorting must support group/movie incidence.
- Movie/group filters should be able to use performer count where useful.
- Sorting must be backed by database queries, not client-only counting.

### Metadata Cleanup

- The UI should make it possible to find movie/group records missing poster art.
- The UI should make it possible to find records missing localized titles.
- Cleanup workflows should avoid destructive bulk changes by default.

### Deployment

- The fork lives under `github.com/jethac/stash`.
- Development happens on feature branches with draft PRs.
- Build artifacts are produced locally or in GitHub Actions.
- Synology deployment pulls a completed Docker image and restarts the container.
- Deployment must include backup and smoke-test steps.

## Success Criteria

- A draft PR exists against the fork containing the movie/localized-title changes.
- Backend tests pass for the new model/API behavior.
- UI type check, lint, and production build pass locally or in CI.
- Synology can run the custom image without building it.
- The user can browse movies by poster, edit localized movie titles, switch displayed title language, and sort performers by movie incidence.

## Open Questions

- Should title language preference become a global user setting or remain per-view until the workflow is proven?
- Should movie posters be uploaded only through existing group image fields, or should there be dedicated poster management UI?
- Should localized scene titles be exposed in the first deployed build or kept to movies/groups first?
- Which registry should publish the custom image: GitHub Container Registry or Docker Hub?
