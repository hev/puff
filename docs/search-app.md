# Local search apps

`tpuff serve` opens a read-only search interface for one namespace, using the
selected environment and the same credential/region precedence as the CLI.
App construction makes no model calls. Database requests use your Turbopuffer
account and its normal billing.

## Development preview

The Go integration is implemented. The shared UI is currently a private
`hev/search-ui` development artifact; it is not bundled in public source yet.
Until that artifact is approved for redistribution, public source builds need
an explicit runtime directory:

```sh
# In an authorized checkout of hev/search-ui:
npm run build:runtime

# In tpuff, point at that build:
go build -o tpuff .
./tpuff serve -n notes --ui-dir /path/to/search-ui/dist-runtime
```

Node is needed to build the UI, not to run it. A maintainer can embed the
verified private build locally:

```sh
python3 scripts/import-search-ui.py /path/to/search-ui/dist-runtime --private
make build
./tpuff serve -n notes
```

Imported assets remain gitignored. Do not force-add private assets to this
public repository. `make check-search-ui` and GoReleaser block release until
an approved artifact with recorded licensing and verified digests is included.
Changing repository visibility or publishing the UI is a separate decision.

## Using the app

```sh
tpuff env add prod
tpuff serve -n notes --fts content
tpuff serve -n notes --palette hev --appearance light --layout table --limit 50
tpuff serve -n notes --port 4387 --no-open
tpuff serve -n notes --app ./search-app.json
```

The listener is loopback-only. The printed URL authorizes the browser tab with
an ephemeral session fragment; it never contains your database credential.
Treat that URL as access to the current local session. Ctrl-C stops the server
and cancels upstream work. Browser opening can be disabled with `--no-open`.

The browser has two main actions: **Search** and **Clear**. Search with an empty
query browses documents. Clear resets the query, every filter, and the results,
and cancels pending work without sending another query. Every supported filter
is visible from startup and initially unrestricted; click its field to set it.
The Turbopuffer palette hides `_hevlayer`-prefixed fields from the schema view,
default filters, displayed results, and automatic search-field selection.
Explicit `--app` bindings or `--fts` can opt a field back in. Other palettes
retain those fields.
There is no initial corpus scan or automatic search. Without a full-text index,
Search still browses documents with any selected attribute filters.

Presentation and query settings live in CLI options, with no browser pickers:

| Option | Default |
| --- | --- |
| `--palette` | `turbopuffer` (`hev` and `grayscale` also supported) |
| `--appearance` | `dark` (`light` also supported) |
| `--layout` | `list` (`table` and `cards` also supported) |
| `--limit` | `25`, allowed range 1–100 |
| `--fts` | Configured content field, then searchable `content`, `body`, `text`, or `title`, then the first searchable field alphabetically |

Explicit CLI options override `--app` configuration. Theme choices stored by the
microsite do not override the app's configured appearance. The app never creates indexes
or writes documents. Text search uses BM25; semantic search, Layer Auto routing,
and generated answers are not enabled in this preview.

Date, scalar string, numeric, and boolean controls follow the live schema.
Filter changes take effect when Search is pressed. Changing a draft cancels the pending search;
previous results remain visible. Boolean false and missing values are distinct.
64-bit integer boundaries travel as decimal strings and are validated and
compiled on the server without passing through a JavaScript number.

String value counts load only when a selector is opened. They count other
attribute filters, excluding that selector, and do not count ranked search
matches. A listing is capped at 100 groups and marked incomplete at the cap;
use exact-value entry for absent values. Values are cached for 30 seconds with
at most 32 scopes. Metadata is validated before each operation, so a search
normally makes a metadata request plus one query. At most four operations run
concurrently per local host, each with a 30-second deadline and no automatic
SDK retries.

Results are capped at 100 per request. Ranked search returns a bounded top-k;
browse follows a signed, request-bound ID continuation. It preserves uint64 IDs
and resets when filters change. Pagination is not a snapshot: concurrent writes
may change subsequent pages. Large integers in displayed rows are serialized as
strings for browser precision. No field is silently converted to a vector query.

## Presentation configuration

**Save app configuration** downloads a definition containing field mappings and
presentation choices, without rows or credentials. Edit it and pass `--app` to
select projected fields, available filters, or image/source mappings:

```json
{
  "version": 1,
  "queryField": "content",
  "fields": ["title", "content", "category", "source_url"],
  "filterFields": ["category"],
  "titleField": "title",
  "sourceField": "source_url",
  "layout": "list",
  "palette": "turbopuffer",
  "appearance": "dark",
  "limit": 25
}
```

Mappings must refer to displayed string fields. Layouts are `list`, `table`,
and `cards`; palettes are `hev`, `grayscale`, and `turbopuffer`. Defaults derive
up to 20 displayable fields and all supported filter fields from the schema.
The configuration can explicitly project up to 50 fields. Vector attributes
are not returned. Image rendering requires an explicit `imageField`; source
links open through an explicit action and the host never fetches source URLs.

Invalid configured bindings fail with a repair message. The browser's refresh
control detects schema changes and retains the visible draft while requesting
a reload. The server independently checks current schema for every request.

`tpuff generate app` is the next planned milestone and is not implemented by
this first serving preview. See [the implementation plan](plans/schema-search-app.md).

## Validation record

Run the isolated browser check after building the binary and installing Python
Playwright and its Chromium browser:

```sh
python3 scripts/check-search-app-browser.py --binary ./tpuff
# Add --ui-dir /path/to/search-ui/dist-runtime for a build without embedded assets.
```

The check uses a local fixture endpoint with a fake credential and performs no
live account queries. Standard Go checks are `make test`, `make vet`, and
`make lint`.

The implementation was checked with the full Go race-test suite, HTTP/SDK
integration tests, and browser interaction tests using the actual compiled
binary and shared components. Live read-only checks on 2026-09-10 compared
three-row queries against direct Turbopuffer calls for `shelf-books` (BM25 plus
numeric filter) and `lens-commons-quality` (ordered browse), including facet
counts and browse continuation. No datasets or credentials were saved.
