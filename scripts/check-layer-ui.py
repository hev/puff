#!/usr/bin/env python3
"""Release gate: a source-installable binary must include an approved UI bundle."""
import hashlib
import json
from pathlib import Path

root = Path(__file__).resolve().parent.parent / 'internal/searchapp/assets'
p = root / 'manifest.json'
if not p.exists():
    raise SystemExit('Release blocked: shared search UI bundle is not included.')
m = json.loads(p.read_text())
if m.get('protocol') != 1 or not m.get('redistributable') or not m.get('license'):
    raise SystemExit('Release blocked: shared UI redistribution and license are not recorded.')
for name, checksum in m['files'].items():
    relative = Path(name)
    if relative.is_absolute() or '..' in relative.parts:
        raise SystemExit('Invalid manifest path')
    if hashlib.sha256((root / relative).read_bytes()).hexdigest() != checksum:
        raise SystemExit('UI digest mismatch: ' + name)
print('Approved search UI bundle verified.')
