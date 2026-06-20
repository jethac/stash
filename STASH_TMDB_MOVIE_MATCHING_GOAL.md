# Stash TMDB Movie Matching Goal

Research date: 2026-06-19

## Objective

Make Stash's `/movies` route behave more like a Plex movie library by adding a Plex-style movie matching pipeline:

- infer movie identity from folder and file names;
- recognize provider IDs such as `{tmdb-272}` and `{imdb-tt0372784}`;
- enrich Stash groups with TMDB metadata where a confident match exists;
- preserve a dry-run and review-first workflow before applying changes.

This goal builds on the current branch's movie work, where Stash groups are used as movie records and `/movies` already renders poster-style group cards.

## Research Findings

- Stash already has a scraper framework for scenes, groups, galleries, and performers. Scrapers can add covers/posters and are managed under `Settings > Metadata Providers`.
- Stash supports multiple scraper styles, including URL, name, fragment, XPath, JSON, CDP, Python, and Ruby scrapers.
- The Stash CommunityScrapers repository already contains TMDB scraper definitions:
  - `scrapers/TMDB.yml`: XPath scraper for `groupByURL` and `sceneByURL`.
  - `scrapers/TMDB-json.yml`: JSON/API scraper for `groupByURL` using TMDB API bearer-token auth.
- The existing community TMDB definitions are URL-driven. They do not by themselves provide the Plex-like step of parsing a folder name, searching candidates, scoring matches, and choosing the best group update.
- The local custom x86_64 Docker image used for branch deployment does not include Python. This does not block the YAML/JSON TMDB scraper, but it argues against making Python scrapers part of the critical path unless the Docker image changes.
- Plex recommends putting movies in their own folders, naming folders after the movie and year, and optionally embedding provider IDs in curly braces such as `{tmdb-272}` or `{imdb-tt0372784}`.
- TMDB API supports direct movie details by TMDB ID, external-ID lookup for IMDb IDs, and movie search by title with year/language filters.
- Stash `GroupCreateInput` and `GroupUpdateInput` can store the fields we need for movie enrichment: name, aliases, duration, date, director, synopsis, URLs, studio, front image, back image, and custom fields. Localized titles already exist on `Group`.

## Target Behavior

Given a library path like:

```text
/data/Porn [FR]/Some Movie (2019) {tmdb-12345}/Some Movie (2019).mkv
/data/Porn [EN]/Another Movie (2020) {imdb-tt1234567}.mkv
/data/Porn [JP]/Japanese Title [1080p]/video.mkv
```

the matcher should:

1. Prefer the parent folder name when a scene appears to be in a movie-specific folder.
2. Parse explicit provider IDs first.
3. Parse a likely title, release year, edition, and leftover tokens from folder/file names.
4. Query TMDB by TMDB ID, IMDb ID, or title plus year.
5. Score candidates using provider ID, title similarity, year/date match, original title, alternative titles, runtime if available, and path hints.
6. Present a dry-run report with the selected candidate, confidence, and planned Stash mutations.
7. Apply only reviewed/high-confidence updates by default.

## Requirements

### Path Hint Parser

- Parse Plex-style provider tokens:
  - `{tmdb-12345}`
  - `{imdb-tt0372784}`
  - `{edition-Director's Cut}`
- Parse common movie filename/folder patterns:
  - `Title (YYYY)`
  - `Title YYYY`
  - `Title [YYYY]`
  - `Title - pt1`, `Title - cd1`, `Title - disc1`
- Strip common quality/source tokens from matching input, while preserving them as diagnostic metadata:
  - `1080p`, `2160p`, `BluRay`, `WEB-DL`, `x264`, `x265`, `HEVC`, audio tags, release group suffixes.
- Prefer folder-level identity over file basename when the folder contains one primary video.
- Store parser output as structured data: title, year, tmdb_id, imdb_id, edition, confidence hints, source path, source folder.

### TMDB Provider

- Use TMDB v3 API with a bearer token configured outside source control.
- Support lookup modes:
  - direct movie details by TMDB ID;
  - external ID lookup for IMDb IDs;
  - title search with optional year and language.
- Fetch and map:
  - title and original title;
  - alternative titles and translations where useful;
  - release date;
  - runtime;
  - overview/synopsis;
  - poster and backdrop image paths;
  - TMDB and IMDb URLs;
  - director from credits;
  - production companies as candidate studios.
- Cache TMDB responses locally during batch runs to avoid repeat API calls and make dry-runs stable.

### Matching Workflow

- Default mode is dry-run.
- Explicit provider ID matches are high confidence but still reviewed for adult/ambiguous content.
- Title/year searches must score candidates and surface alternatives rather than silently choosing weak matches.
- Existing curated Stash metadata must not be overwritten unless the caller enables an overwrite mode.
- Record applied metadata provenance in `custom_fields`, for example:
  - `movie_match_source: "tmdb"`
  - `movie_match_tmdb_id: "12345"`
  - `movie_match_confidence: 0.98`
  - `movie_match_applied_at: "2026-06-19T..."`

### Stash Updates

- Reuse existing groups where possible.
- Add/update group URLs with TMDB and IMDb URLs.
- Update group front image from TMDB poster and back image from backdrop when allowed.
- Update localized titles without losing existing Plex-derived titles:
  - English title;
  - original title;
  - optional Japanese/French translated titles when TMDB has them.
- Link matched groups to scenes when the source path maps to an existing Stash scene.
- Keep scene-level performer/tag metadata separate from movie-level TMDB metadata unless explicitly added in a later phase.

## Implementation Strategy

### Phase 1: Workstation/Ubicloud Batch Matcher

Create a new Go helper, likely `scripts/movie_match`, that runs on the workstation or Ubicloud build host and talks to Synology Stash over GraphQL.

This is the lowest-risk path because:

- it avoids running heavy matching work on the Synology CPU;
- it avoids depending on Python in the custom Docker image;
- it can reuse patterns from `scripts/group_import`;
- it can be tested with fixtures and dry-run output before touching the Stash database.

Expected commands:

```powershell
$env:TMDB_BEARER_TOKEN = "..."
go run ./scripts/movie_match `
  --endpoint http://192.168.1.112:9999/graphql `
  --root "/data/Porn [FR]" `
  --cache ./.cache/tmdb `
  --dry-run
```

```powershell
go run ./scripts/movie_match `
  --endpoint http://192.168.1.112:9999/graphql `
  --root "/data/Porn [FR]" `
  --cache ./.cache/tmdb `
  --min-confidence 0.90 `
  --apply
```

### Phase 2: In-App Review UI

Add a movie matching review surface in Stash:

- unmatched groups/scenes queue;
- candidate list with posters, dates, original titles, and confidence;
- apply/skip controls;
- field-level checkboxes for what will be updated;
- bulk apply for high-confidence matches.

Implemented direction:

- `/movies/match` can import a JSON review report from `scripts/movie_match`.
- `/movies/match` can also call the native `movieMatchPlan` GraphQL query to generate a dry-run review plan directly from Stash scenes under configured roots.
- `movieMatchPlan` uses `TMDB_BEARER_TOKEN` or `TMDB_API_READ_ACCESS_TOKEN` from the server environment by default, with an optional request-level token override for ad hoc testing.
- The review UI applies selected rows through existing Stash mutations, preserving the review-first workflow instead of creating an automatic write path.
- Group create/update rows expose field-level checkboxes for the planned mutation payload, with required create fields and `movie_match_*` provenance preserved.
- Match rows show poster thumbnails, release-year/title context, original title when different, confidence, and alternate candidate scores.
- The review page includes bulk controls to select/clear actionable rows and select/clear optional group fields before applying.
- Rows can be explicitly skipped/unskipped during review; skipped rows are not applied.
- Live deployment and end-to-end validation steps are tracked in `STASH_TMDB_LIVE_VALIDATION.md`.

### Phase 3: Optional Scraper Integration

Evaluate whether to install or adapt the CommunityScrapers TMDB scraper after the matcher exists.

Potential uses:

- URL scrape when a group already has a TMDB URL;
- fallback manual scrape from the group edit page;
- compare community scraper output against the custom TMDB client.

Do not make the URL-only scraper the main Plex-like matcher. It is useful metadata plumbing, not the identity resolver.

## Non-Goals

- Replacing Plex completely.
- TV show/episode matching.
- Automatic writes for weak or ambiguous matches.
- Overwriting manually curated titles, posters, URLs, or synopsis by default.
- Treating TMDB as authoritative for adult/JAV metadata, where coverage may be poor or absent.

## Risks

- TMDB coverage for adult titles may be incomplete, inconsistent, or missing.
- Some adult titles may have aliases that collide with mainstream films.
- Production company mapping to Stash studios may create noisy studios if not reviewed.
- TMDB image attribution and API terms should be checked before broad UI exposure.
- Existing `TMDB-json.yml` in CommunityScrapers appears to use URL-driven API calls and requires manual bearer-token configuration; it should be validated before installation. The checked copy maps `Name` to `titlez`, which looks like a typo or intentional placeholder and may need correction before use.
- Matching by folder name alone can be wrong for compilation folders, scene collections, and poorly named downloads.

## Acceptance Criteria

- A test fixture set of representative current-library paths parses provider IDs, title, year, and edition correctly.
- Dry-run output shows:
  - source path;
  - parsed hints;
  - TMDB query mode;
  - selected candidate;
  - alternate candidates;
  - confidence score;
  - planned Stash field changes.
- Explicit `{tmdb-*}` paths resolve without title search.
- Explicit `{imdb-tt*}` paths resolve through TMDB external-ID lookup.
- Title/year paths search TMDB and decline weak matches below the configured threshold.
- Applying a high-confidence match updates the group record, images, URLs, localized titles, and scene link without clobbering existing curated fields unless overwrite is enabled.
- `/movies` displays the resulting poster, title, year/date, and localized title behavior from the updated group metadata.

## Sources

- Stash scraper docs: https://docs.stashapp.cc/metadata-sources/scrapers/
- Stash CommunityScrapers: https://github.com/stashapp/CommunityScrapers
- Local CommunityScrapers files checked: `scrapers/TMDB.yml`, `scrapers/TMDB-json.yml`
- Plex movie naming guide: https://support.plex.tv/articles/naming-and-organizing-your-movie-media-files/
- TMDB movie search API: https://developer.themoviedb.org/reference/search-movie
- TMDB movie details API: https://developer.themoviedb.org/reference/movie-details
- TMDB external-ID lookup API: https://developer.themoviedb.org/reference/find-by-id
- Local Stash schema checked: `graphql/schema/types/group.graphql`, `graphql/schema/types/scraped-group.graphql`
- Existing helper checked: `scripts/group_import/README.md`
- Local Docker note checked: `docker/build/x86_64/README.md`
