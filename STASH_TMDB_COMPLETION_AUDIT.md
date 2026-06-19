# Stash TMDB Movie Matching Completion Audit

Audit date: 2026-06-19

This audit checks `STASH_TMDB_MOVIE_MATCHING_GOAL.md` against the current branch and live Synology deployment. Completion is not claimed unless the evidence below proves the requirement.

## Current State

- Branch: `jethac/media-library-improvements`
- Latest pushed commit: `fc7ad88e`
- Live Synology Stash endpoint: `http://192.168.1.112:9999/graphql`
- Live deployed version: `custom-tmdb-movie-match`
- Live deployed hash: `tmdb-movie-match-20260619`
- Live GraphQL schema exposes `movieMatchPlan`.
- No valid TMDB bearer token is available in this shell or deployed container.

## Requirement Audit

| Requirement | Evidence | Status |
| --- | --- | --- |
| Parse Plex-style `{tmdb-*}`, `{imdb-tt*}`, and `{edition-*}` tokens. | `pkg/moviematch/parse.go`; parser fixtures in `pkg/moviematch/parse_test.go`; CLI parser tests in `scripts/movie_match/main_test.go`. | Proven locally. |
| Parse common movie path patterns and strip quality/source tokens. | `TestParseHintRepresentativePathFixtures`, `TestParseHintUsesStandaloneFileAndStripsQualityTokens`, and CLI equivalents. | Proven locally. |
| Prefer folder-level identity over file basename. | Parser implementation and tests using movie folders with child video files. | Proven locally for representative fixtures. |
| Query TMDB by TMDB ID without title search. | `TestTMDBClientFindCandidatesByTMDBIDSkipsSearch`. | Proven locally with fake TMDB server. |
| Query TMDB by IMDb external ID. | `TestTMDBClientFindCandidatesByIMDbIDUsesExternalLookup`. | Proven locally with fake TMDB server. |
| Query TMDB by title and year. | `TestTMDBClientFindCandidatesByTitleAndYearUsesSearch`. | Proven locally with fake TMDB server. |
| Fetch and map title, original title, release date, runtime, overview, poster, backdrop, IMDb/TMDB URLs, director, companies, alternates, and translations. | `pkg/moviematch/tmdb.go`; TMDB details fixture in `pkg/moviematch/tmdb_test.go`; apply workflow test validates fields consumed by mutations. | Proven locally for representative API responses. |
| Cache TMDB responses during batch runs. | `TestTMDBClientCachesResponses`. | Proven locally. |
| Default to dry-run and preserve review-first workflow. | CLI defaults to report mode; native API `movieMatchPlan` returns dry-run items only; UI applies through explicit selected rows. | Proven by code and tests. |
| Score candidates and decline weak title/year matches. | `pkg/moviematch/score.go`; `TestScoreCandidatesDeclinesWeakTitleYearMatch`; planner uses `min_confidence`. | Proven locally. |
| Surface alternates in dry-run output. | CLI report model and native GraphQL include `alternates`; UI renders alternate candidate scores. | Proven by code and generated schema; not visually verified in a browser with real TMDB data. |
| Do not overwrite curated fields unless overwrite is enabled. | `TestMovieMatcherApplyUpdatesExistingGroupConservatively`; `TestMovieMatchGroupInputConservativeUpdateKeepsCuratedFields`; overwrite test. | Proven locally. |
| Record provenance custom fields. | CLI and API mutation input builders add `movie_match_*`; `TestMovieMatcherApplyCreatesGroupTitlesAndSceneLink` validates provenance values. | Proven locally. |
| Reuse existing groups by scene group, URL, or exact name. | CLI and API planner code paths search scene groups, TMDB/IMDb URLs, then name. | Proven by code inspection; partial local coverage for existing scene group path. |
| Add/update group URLs, front image, back image, date, duration, synopsis, and director. | `TestMovieMatcherApplyCreatesGroupTitlesAndSceneLink`; API group input tests. | Proven locally for CLI path and shared mapping behavior. |
| Update localized titles without losing existing titles. | CLI/API title planning upserts missing titles and reports conflicts unless overwrite; apply test validates title creation for `en`, `fr`, `ja`, and `original`. | Proven locally. |
| Link matched groups to scenes. | CLI apply test validates scene link; native UI applies `sceneUpdate` with existing groups preserved plus new group. | Proven locally for CLI, code-inspected for UI. |
| Keep scene-level performer/tag metadata separate. | Matcher only mutates groups, localized titles, and scene group links; no performer/tag mutation path is present. | Proven by code inspection. |
| Provide workstation/CI helper. | `scripts/movie_match` and README. | Implemented and tested. |
| Provide in-app review UI. | `/movies/match` route, `MovieMatchReview.tsx`, GraphQL query, field-level controls, skip/apply controls. | Implemented and type/lint checked. |
| Deploy to Synology. | `STASH_TMDB_LIVE_VALIDATION.md`; live version/schema checks. | Proven for deployed image `tmdb-movie-match-20260619`. |
| Native live dry-run with real TMDB data. | Live resolver accepts token override and reaches TMDB with invalid token; valid token is unavailable. | Not proven. |
| Apply one reviewed high-confidence live match and verify `/movies` render. | Requires a valid TMDB token and selecting a real match. | Not proven. |

## Validation Commands Run

```powershell
git diff --check
go test ./pkg/moviematch ./scripts/movie_match ./scripts/...
docker run --rm -v gomodcache:/go/pkg/mod -v gocache:/root/.cache/go-build -v "${PWD}:/workspace" -w /workspace golang:1.25 bash -lc 'export PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; go test ./internal/api -run MovieMatch'
npm exec -- tsc --noEmit
npm exec -- biome lint src/components/Groups/MovieMatchReview.tsx src/components/Groups/Groups.tsx src/components/Groups/GroupList.tsx
npm exec -- stylelint "src/components/Groups/styles.scss"
```

## Live Smoke Evidence

- Live schema check: `movieMatchPlan` is present.
- Live no-token request fails closed before network access:
  - `missing TMDB token; set TMDB_BEARER_TOKEN on the server or pass tmdb_token`
- Live invalid-token request scans `/data`, parses the first scene path, calls TMDB, and returns the expected TMDB 401 inside a dry-run error item.

## Remaining Work Before Completion

1. Provide a valid TMDB v3 read-access bearer token through the `/movies/match` override field or Synology container environment.
2. Run a native dry-run against a small root, ideally a folder with `{tmdb-*}` or `{imdb-tt*}` in the path.
3. Confirm the review table shows parsed hints, chosen candidate, alternates if present, confidence, and planned group/title/link actions.
4. Apply one reviewed high-confidence match.
5. Verify the resulting Stash group has TMDB/IMDb URLs, poster/backdrop images where selected, metadata fields, `movie_match_*` custom fields, localized titles, and a scene group link.
6. Verify `/movies` renders the resulting poster/title/year/localized title behavior.

Until those live-token checks pass, the goal remains incomplete.
