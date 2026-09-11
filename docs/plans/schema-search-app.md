# Schema-generated search apps in tpuff

Status: implementation started, 2026-09-10. `serve` is implemented and validated
locally; public UI distribution and `generate app` remain pending. Original
source inspected at `471b2a8` on `main`. See [preview status](../search-app.md).

## Outcome

An existing Turbopuffer user runs `tpuff serve -n notes` and gets a working
search app over that namespace in their browser. App construction is
deterministic: schema, explicit capabilities, and presentation mappings select
the interface. It requires no LLM, model API key, Layer account, or hosted
builder. Retrieval uses the user's configured Turbopuffer account; ordinary
database charges still apply. No requests or model costs pass through hev.

Ship local serving first. Follow with `tpuff generate app` for an editable
project. Both consume the same Layer UI implementation and versioned app
definition. The hosted builder is a later consumer, outside this repo's scope.
This work does not block the Layer 0.6 infrastructure release.

## User flow and proposed CLI

```sh
# Existing setup; reuse the selected environment and credential precedence.
tpuff env add prod

# Phase 1: open a local search application.
tpuff serve -n notes
tpuff serve -n notes --fts content --port 4387 --no-open
tpuff serve -n notes --app ./search-app.json

# Phase 2: generate editable source, without installing or running it.
tpuff generate app -n notes --dir ./my-search
tpuff generate app -n notes --app ./search-app.json --dir ./my-search
```

`-n/--namespace` is required; `-r/--region` retains the existing spelling.
`serve` binds to `127.0.0.1`, uses an available port by default, prints the URL,
and opens the browser unless `--no-open` is set. An explicitly occupied port
returns an actionable error. Ctrl-C cancels upstream requests and shuts down.
Browser-launch failure leaves the server usable at its printed URL. Machine
output follows the CLI's existing `--output plain` convention.

Choose the query field in this order: validated `--fts`, app configuration,
compatible configured content field, then the only eligible full-text field.
With several eligible fields and no explicit choice, show a field selector.
With none, show ordered document browsing and explain how to enable text search;
do not mutate the namespace schema. Starting the server never scans the corpus
or submits a search automatically. The user explicitly requests browse/results.

## Scope of the first usable release

- Live schema inspection; text search using BM25; ordered browse by ID.
- Date, number, boolean, and scalar-string filters where supported. Multiple
  predicates must actually reach Turbopuffer with their original typed values.
- List and table results with field selection, stable IDs, and source detail.
  Image cards are enabled by an explicit image mapping; generic rows work without
  mappings. Hev/grayscale palettes and light/dark appearance use shared UI tokens.
- Draft/applied query state, clear filters, loading/error/empty states,
  cancellation, and rejection of stale responses.
- Explicit attribute projection and request limits. Unknown schema types remain
  inspectable but do not silently become supported controls.
- On-demand bounded scalar value counts using the backend's grouped aggregation
  support. Counts describe the attribute-filter scope, excluding the facet's own
  selection, not relevance-ranked search results. Label that scope. Mark capped
  value listings as incomplete; permit exact-value entry when a value is absent.
  Cache by namespace/schema/filter scope; never derive corpus counts from hits.
- Browse continuation uses backend-supported ordered ID filtering, preserving
  numeric versus string IDs losslessly. Ranked BM25 results use a bounded top-k
  with an explicit result limit; do not fabricate a Layer cursor or ranked
  pagination contract. A filter/query change resets continuation.

ANN, hybrid routing, summaries/chat, inferred semantic mappings, and hosted
publication are follow-ons. A vector column alone does not identify its query
embedding model. No paid or downloaded model is invoked implicitly. Future
model-backed features require explicit user configuration and user-owned keys
or local compute. The basic app remains useful without them.

## Architecture and ownership

### Shared Layer UI package (external dependency)

The proposed `hevlayer-ui` npm package owns schema-driven component selection,
shared query state, filter compilation, result presentation, and themes. Its
core is independent of the Layer client. Adapters describe supported operations;
the Turbopuffer path must not emit Layer Auto/HybridText, scan jobs, history, or
`next_cursor` requests. npm names and exports are proposals until published.

The current local design study is `lyr/hev-search-concept` (the `lyr-5` tmux
session). It is fixture-driven, not a distributable package. Before tpuff can
ship, the UI owner must produce a public, compatible, redistributable release
with a generic HTTP adapter, usable without the private Layer repository.
Agree on artifact licensing and notices before bundling. Do not copy the study
into tpuff or implement a second set of UI controls here.

### Go host in tpuff

Add thin Cobra commands in `cmd/serve.go` and later `cmd/generate.go`.
An `internal/searchapp` package owns configuration validation, the HTTP host,
the direct Turbopuffer adapter, request bounds, and shutdown. Reuse
`internal/client` credential/endpoint resolution and the SDK; do not call
command handlers as library functions.

Expose narrow local operations for bootstrap/schema, query/browse, and values,
all fixed to the namespace and environment selected at startup. The browser
cannot choose an upstream URL, credentials, arbitrary namespace, or write
operation. Keep keys out of HTML, logs, URLs, app definitions, and generated
source. Validate Host/Origin and require a per-launch session token for data
API calls so another website cannot use the authenticated localhost service.
Bundle fonts and static assets; use explicit user actions for external sources.
Server-side source fetching is outside v1; URLs are not permission to fetch.

### Single binary distribution

Consume a pinned UI build and embed its static files with Go `embed.FS`.
Check the generated asset bundle, version/digest manifest, and required notices
into tpuff so a tagged `go install github.com/hev/tpuff@<tag>` also works from
the public module source with Go alone. A maintainer refresh script rebuilds or
fetches the exact verified upstream artifact. No runtime CDN, npm install, model
download, or update fetch is needed for serving. Node is a maintainer build tool,
not an end-user requirement for `serve`.

Keep GoReleaser's existing Linux/macOS amd64/arm64 binary flow. Release checks
verify the embedded UI is present and matches the recorded UI version. The
generated bundle is derived from the shared package, never hand-edited.

### Portable app definition

Use a versioned `search-app.json` for query field, approved displayed fields,
filters, layout, theme, and title/image/source mappings. Validate bindings
against the live schema on each launch; preserve invalid drafts for repair.
Exclude keys, rows, and inferred private sample content. Source connection
secrets stay in the host's environment/configuration. Include package/config
compatibility versions. The same definition must feed local serving and the
generated project; future hosted-builder support must preserve this contract.

## Existing code to reuse or avoid

- `internal/client/client.go` resolves API key, region, and optional base URL;
  `internal/config/config.go` stores environment and per-namespace content fields.
  Freeze one resolved environment per server process. The current client cache
  is keyed by endpoint/region, so do not introduce multi-account switching within
  a running host without separately correcting credential-aware cache identity.
- `cmd/schema.go` already reads namespace metadata. Reuse the SDK operation and
  schema utilities, keeping server errors returnable rather than using `os.Exit`.
- `cmd/search.go` parses `--filters` but does not attach them to query params.
  Track that existing defect separately; the new adapter must send and verify
  filters directly and must not inherit the CLI's dropped-filter behavior.
- `cmd/scan.go` reads every document and stringifies IDs. Do not use it for app
  startup, automatic facets, or typed browse pagination.
- `.goreleaser.yml`, `Makefile`, and `.github/workflows/ci.yml` define the current
  binary and verification lanes; extend them only where this feature needs it.

## Implementation sequence and acceptance

1. **UI artifact and adapter contract.** Settle the minimal bootstrap, query,
   values, error, cancellation, and app-definition shapes with the UI owner.
   Pin a public UI artifact. Accept: one shared UI runs through a mock HTTP host;
   unsupported capabilities are absent and credentials are never UI inputs.
2. **Local serving.** Implement configuration resolution, schema bootstrap,
   embedded assets, loopback host, session protection, and clean shutdown.
   Accept: a downloaded binary opens the interface with no Node installed;
   missing credentials, missing namespaces, and occupied ports fail clearly.
3. **Live search and filters.** Wire BM25, typed predicates, result projection,
   ordered browse, bounded facet counts, request cancellation, and schema changes.
   Accept: direct-to-Turbopuffer queries match equivalent SDK requests for two
   different real schemas; no Layer or model credentials are configured. Check
   text/numeric/boolean/date filters, missing values, and numeric/string IDs.
4. **Package and document the preview.** Verify assets/notices and release
   reproducibility, document commands and exact supported capabilities, and add
   the README quickstart. Accept: install a release candidate into a clean
   environment and follow the docs to a useful query without a private checkout.
   Record setup time and interventions. Model-provider/hev egress is absent;
   only the configured database receives data requests.
5. **Editable app generation (after serve acceptance).** Emit a minimal
   React/TypeScript project with pinned UI/client dependencies, lockfile,
   server adapter, app definition, `.env.example`, `.gitignore`, and run/build
   instructions. Template generation is deterministic and makes no LLM calls.
   Refuse nonempty output directories; stage output before final placement.
   Do not install dependencies, start a server, deploy, or overwrite user edits.
   Accept: generate twice from the same inputs with equivalent outputs, inspect
   for secrets/data, install and build separately, then execute the same queries
   as `serve`. The generated server starts on localhost; production hosting and
   authentication remain explicit deployment work, not a claim of this scaffold.

## Validation and handoff

Implementation verification uses the existing `make test` (race detector),
`make vet`, `make lint`, and `make build`. Add meaningful HTTP/SDK integration
tests that capture outgoing predicates and selected namespace, test localhost
access protections and redaction, exercise cancellation, and bound facet work.
Browser checks cover filter composition, stale responses, keyboard access,
schema failure, and responsive results using the actual bundled UI.

Use authored test fixtures, not checked-in customer datasets. Live acceptance
uses an explicitly selected namespace read-only; record aggregate outcomes, not
credentials or document bodies. The serving implementation now has SDK integration and browser checks plus
read-only parity runs on two live namespaces; see the preview status for details.
Public packaging and app generation remain open in the repository's Beads tracker.

Tracking: epic `tpuff-8gm`; phases 1–5 are `tpuff-8gm.1` through
`tpuff-8gm.5`, with each phase depending on the previous one. The existing
dropped-filter defect is `tpuff-0ai`. Beads is local/gitignored in this checkout;
the phase descriptions and acceptance criteria above are the portable record.

## References

- [tpuff CLI and installation](https://github.com/hev/tpuff)
- [Turbopuffer namespace metadata](https://turbopuffer.com/docs/metadata)
- [Turbopuffer query contract](https://turbopuffer.com/docs/query)
- [Go embedded files](https://pkg.go.dev/embed)

Backend-specific behavior must be checked against the pinned Go SDK and these
contracts during implementation. Schema similarity is not operator parity.
