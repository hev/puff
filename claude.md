# Turbopuffer CLI - Future Features

This document tracks planned features and improvements for the puff CLI.

## Commands to Implement

### List Command Enhancements
- [ ] Implement document listing within a namespace
- [ ] Add filtering options for namespace listing
- [ ] Add pagination support for large result sets
- [ ] Add output formatting options (JSON, table, etc.)

### Browse Command
- [ ] Interactive browsing of namespace data
- [ ] Display document details
- [ ] Navigate through documents

### Full-Text Search (FTS)
- [ ] Implement FTS command for searching within namespaces
- [ ] Support various search query syntaxes
- [ ] Display search results with relevance scores

### Admin Commands
- [ ] **Backup**: Create backups of namespaces
  - Export namespace data
  - Save to local file or remote storage
  - Support incremental backups

- [ ] **Add Filterable Properties**: Manage filterable properties for namespaces
  - Add new filterable properties
  - List existing properties
  - Remove properties

- [ ] **Upsert by Filter**: Update or insert single values by filter
  - Support complex filter expressions
  - Validate data before upsertion

- [ ] **Warm Cache**: Pre-load data into cache
  - Warm specific namespaces
  - Configure cache warming strategies

- [ ] **Evaluate Recall**: Test and evaluate recall metrics
  - Run recall evaluation tests
  - Display metrics and performance stats

## Technical Improvements
- [ ] Add comprehensive error handling
- [ ] Implement logging with configurable verbosity
- [ ] Add unit tests
- [ ] Add integration tests
- [ ] Create detailed documentation
- [ ] Add configuration file support (.puffrc)
- [ ] Implement retry logic for API calls
- [ ] Add progress indicators for long-running operations
- [ ] Support multiple output formats (JSON, YAML, CSV, table)
- [ ] Add shell completion support (bash, zsh, fish)

## User Experience
- [ ] Add interactive prompts for complex operations
- [ ] Implement confirmation prompts for destructive operations
- [ ] Add colored output for better readability
- [ ] Create help examples for each command
- [ ] Add verbose mode for debugging

## Documentation
- [ ] Complete README with usage examples
- [ ] Add API documentation
- [ ] Create tutorial videos/guides
- [ ] Document environment variables
- [ ] Add troubleshooting guide
