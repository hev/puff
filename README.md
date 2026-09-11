# puff

An unofficial Go CLI and TUI for [turbopuffer](https://turbopuffer.com).

## Install

### Rename transition

The project and command are now `puff` (formerly `tpuff`). Until the first
renamed release is tagged, install from `main` using the command below.
Existing release assets retain their original names.

On first use, puff copies an existing `~/.tpuff/config.toml` to
`~/.puff/config.toml` with private permissions. An existing puff configuration
takes precedence. `PUFF_EMBEDDING_URL` replaces `TPUFF_EMBEDDING_URL`; the older
variable remains a fallback.

Future tagged releases publish prebuilt `puff` binaries to
[GitHub Releases](https://github.com/hev/puff/releases) and the `hev/homebrew-tap`
cask. The release workflow requires a cross-repository `HOMEBREW_TAP_TOKEN`.

### From source

```bash
go install github.com/hev/puff@main
```

Or clone and build:

```bash
git clone https://github.com/hev/puff
cd puff
make install    # builds and installs to $(go env GOBIN) or $GOPATH/bin
```

## Quick start

```bash
# First-time setup (prompts for API key + region)
puff env add prod

# Launch the interactive browser
puff            # equivalent to `puff browse`
```

> `TURBOPUFFER_API_KEY` / `TURBOPUFFER_REGION` env vars override the config
> file. A `-r <region>` flag on most commands overrides the env's region.

## Feature highlights

### Interactive TUI browser

Running `puff` opens a keyboard-driven terminal UI. Browse environments,
namespaces, documents, and schemas without leaving the terminal. Full-text
search is built in — press `/` in the documents view to BM25 search inline.

### Local search app (development preview)

`puff serve -n my-namespace` adds a browser interface generated from the namespace
schema. The Go host is implemented; the shared UI bundle remains private pending
redistribution approval. See [local search apps](docs/search-app.md) for the
`--ui-dir` development flow, supported operations, and local embedding.

### Full-text and vector search

```bash
# BM25 full-text search
puff search "pulmonary edema" -n notes --fts content

# Vector similarity search via an embedding model
puff search "renal failure" -n notes -m sentence-transformers/all-MiniLM-L6-v2
```

### Scan — extract unique field values

`scan` paginates through every document in a namespace and collects the
distinct values of a field, like a `SELECT DISTINCT` for turbopuffer. Useful
for understanding the shape of your data or building filter UIs.

```bash
puff scan -n my-namespace --field category
# streams progress to stderr, outputs a sorted JSON array to stdout
```

### Schema management

Copy schemas between namespaces, apply them from a file, or bulk-apply to
every namespace at once.

```bash
puff schema copy --from ns-a --to ns-b
puff schema apply --all -f schema.json
```

### Edit documents in `$EDITOR`

```bash
puff edit doc_abc123 -n my-namespace
```

Opens the document as JSON in your editor; saving writes the changes back.

### Prometheus exporter

Expose namespace metrics for Grafana/Prometheus. See
[monitoring.md](./monitoring.md) for scrape config and Docker deployment.

```bash
puff export              # exporter on :9876
puff export -A           # scrape all regions
```

### Multi-environment config

Manage multiple API keys and regions from `~/.puff/config.toml`. Switch with
`puff env use <name>` or interactively in the TUI.

## Run `puff --help` for the full command reference.

## License

MIT — see [LICENSE](./LICENSE).
