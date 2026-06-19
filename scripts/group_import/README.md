# Group Import

This helper creates Stash groups as movie records through GraphQL. It is intended for workstation or CI use, not for the Synology NAS.

The script is dry-run by default. It only writes when `--apply` is present. It never updates existing group metadata; if a group already exists by ID or exact name, the group is reused and only localized title rows are planned or applied. Existing different localized titles are reported as conflicts unless `--overwrite` is present.

## CSV Format

Supported headers:

- `name`: group/movie name. Aliases: `group_name`, `movie_name`, `title`.
- `id`: optional existing Stash group ID. Aliases: `group_id`, `movie_id`.
- `aliases`
- `duration`: seconds. Alias: `duration_seconds`.
- `date`: `YYYY-MM-DD`; a bare year is normalized to `YYYY-01-01`. Aliases: `release_date`, `year`.
- `rating100`: integer 1-100. Aliases: `rating_100`, `rating`.
- `studio_id`
- `director`
- `synopsis`: aliases: `description`, `summary`.
- `urls`: `|` or `;` separated. Alias: `url`.
- `tag_ids`: `|` or `;` separated.
- `front_image`: URL or base64 data URL. Aliases: `poster`, `poster_url`.
- `back_image`: URL or base64 data URL. Aliases: `back_poster`, `back_image_url`.
- `source`: localized title source, defaults to `--source`.
- `title_en`, `title_ja`, `title_fr`, `title_native`: localized title columns. `localized_title_<lang>` and `name_<lang>` aliases are also supported.

Example:

```csv
movie_name,year,poster_url,url,title_ja,title_en,source
Movie A,2024,http://example/poster-a.jpg,http://example/movie-a,日本語タイトル,English Title,plex
```

## JSON Format

The JSON input is an array using the same field names and aliases:

```json
[
  {
    "movie_name": "Movie A",
    "year": "2024",
    "poster_url": "http://example/poster-a.jpg",
    "url": "http://example/movie-a",
    "title_ja": "日本語タイトル",
    "title_en": "English Title",
    "source": "plex"
  }
]
```

## Commands

Dry-run:

```powershell
go run ./scripts/group_import `
  --endpoint http://192.168.1.112:9999/graphql `
  --input .\groups.csv
```

Apply:

```powershell
$env:STASH_API_KEY = "your-api-key"
go run ./scripts/group_import `
  --endpoint http://192.168.1.112:9999/graphql `
  --input .\groups.csv `
  --apply
```

Apply and replace existing different localized titles:

```powershell
go run ./scripts/group_import `
  --endpoint http://192.168.1.112:9999/graphql `
  --input .\groups.csv `
  --apply `
  --overwrite
```

The output reports group creation, existing group matches, localized title creates/updates/conflicts, duplicate input rows, and row-level errors.
