#!/usr/bin/env python3
"""Verify and stage an upstream UI build for embedding. Never edits UI source."""
import argparse
import hashlib
import json
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('bundle', type=Path)
parser.add_argument('--private', action='store_true', help='Allow a private bundle for a local build only; assets remain gitignored')
args = parser.parse_args()
root = Path(__file__).resolve().parent.parent
manifest = json.loads((args.bundle / 'manifest.json').read_text())
if manifest.get('protocol') != 1 or not manifest.get('version'):
    parser.error('unsupported bundle manifest')
if not manifest.get('redistributable', False) and not args.private:
    parser.error('bundle is not approved for redistribution; use --private only for local development')
files = manifest.get('files', {})
if 'index.html' not in files or len(files) > 100:
    parser.error('invalid asset listing')
contents = {}
for name, checksum in files.items():
    if name == 'NOTICE.txt':
        parser.error('upstream must use a distinct notice filename')
    relative = Path(name)
    if relative.is_absolute() or any(p in ('..', '.') or p.startswith('.') for p in relative.parts) or relative.suffix not in ('.html', '.js', '.mjs', '.css', '.txt'):
        parser.error('invalid asset path')
    data = (args.bundle / relative).read_bytes()
    if len(data) > 5 * 1024 * 1024 or hashlib.sha256(data).hexdigest() != checksum:
        parser.error('invalid size or checksum: ' + name)
    contents[name] = data
if sum(map(len, contents.values())) > 10 * 1024 * 1024:
    parser.error('bundle exceeds size limit')
if not args.private and not manifest.get('license'):
    parser.error('redistributable artifacts must identify their license and include notices')
# Remove only files named by the previous imported manifest. Keep the tracked
# placeholder notice and all unrelated local files intact.
destination = root / 'internal/searchapp/assets'
old = destination / 'manifest.json'
if old.exists():
    previous = json.loads(old.read_text())
    for name in previous.get('files', {}):
        relative = Path(name)
        if relative.is_absolute() or '..' in relative.parts or name == 'NOTICE.txt':
            continue
        target = destination / relative
        if target.is_file():
            target.unlink()
for name, data in contents.items():
    target = destination / name
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(data)
old.write_text(json.dumps(manifest, indent=2) + '\n')
print('Verified UI ' + manifest['version'] + ' staged for local Go embedding. Imported assets are gitignored.')
