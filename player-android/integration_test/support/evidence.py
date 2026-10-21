#!/usr/bin/env python3
"""Structured per-feature, per-platform evidence for the music E2E runners.

Every runner (Playwright in Chrome and Firefox, the adb runner on Android)
appends one JSON object per check to <evidence>/<platform>/evidence.jsonl
and keeps screenshots, audio captures and state dumps next to it. The
Playwright helper (player-server/test/e2e-web/tests/helpers/music-fixture.ts)
writes the same schema, so `evidence.py summary <dir>` can print one
scenario x platform matrix (latest result of each) for a whole run.

Record fields:

  schema     1
  time       ISO 8601 UTC
  platform   chrome | firefox | android | fixture
  feature    candidate id or area, e.g. "V01 baseline", "M21 gapless"
  scenario   what was exercised, in words
  result     pass | fail | gap   (gap = could not be exercised here; say why)
  proof      real | service-double | none
             "service-double" marks results obtained against a local double
             of an external service: never acceptance of a real integration
  detail     short human-readable explanation
  data       measured values (positions, levels, ids, versions)
  artifacts  paths relative to the evidence directory

The evidence directory lives outside the repository and outside the
fixture's media root (see music_fixture.py), so artifacts survive the
fixture's teardown and are never committed.

Usage: evidence.py summary <evidence-dir>
"""

import datetime
import json
from pathlib import Path
import sys

RESULTS = ('pass', 'fail', 'gap')
PROOFS = ('real', 'service-double', 'none')


class Evidence:
    """Append-only evidence log for one platform of one run."""

    def __init__(self, directory, platform, versions=None):
        self.root = Path(directory)
        self.platform = platform
        self.directory = self.root / platform
        self.directory.mkdir(parents=True, exist_ok=True)
        self.log = self.directory / 'evidence.jsonl'
        if versions is not None:
            (self.directory / 'versions.json').write_text(json.dumps(versions, indent=1))

    def path(self, name):
        """Absolute path for a new artifact of this platform."""
        return self.directory / name

    def record(self, feature, scenario, result, detail='', data=None, artifacts=(), proof='real'):
        """Append one check; returns the record. Raises on invalid values."""
        if result not in RESULTS:
            raise ValueError(f'result must be one of {RESULTS}, not {result!r}')
        if proof not in PROOFS:
            raise ValueError(f'proof must be one of {PROOFS}, not {proof!r}')
        entry = {
            'schema': 1,
            'time': datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='seconds'),
            'platform': self.platform, 'feature': feature, 'scenario': scenario,
            'result': result, 'proof': proof, 'detail': detail, 'data': data or {},
            'artifacts': [str(Path(a).resolve().relative_to(self.root.resolve()))
                          if Path(a).is_absolute() else str(a) for a in artifacts],
        }
        with self.log.open('a', encoding='utf-8') as file:
            file.write(json.dumps(entry, ensure_ascii=False) + '\n')
        return entry


def load(directory):
    """All records below an evidence directory, in file order."""
    records = []
    for log in sorted(Path(directory).glob('*/evidence.jsonl')):
        for line in log.read_text(encoding='utf-8').splitlines():
            if line.strip():
                records.append(json.loads(line))
    return records


def summarize(records):
    """{(feature, scenario): {platform: result}} from the latest record of each.

    A rerun appends new records instead of rewriting old ones, so only the
    most recent result of a scenario on a platform counts.
    """
    matrix = {}
    for record in records:
        matrix.setdefault((record['feature'], record['scenario']), {})[record['platform']] = record['result']
    return matrix


def main(argv):
    if len(argv) != 2 or argv[0] != 'summary':
        sys.exit(__doc__.rsplit('Usage: ', 1)[1])
    records = load(argv[1])
    matrix = summarize(records)
    platforms = sorted({r['platform'] for r in records})
    print('feature · scenario'.ljust(72) + ''.join(p.ljust(9) for p in platforms))
    for feature, scenario in sorted(matrix):
        cells = matrix[(feature, scenario)]
        label = f'{feature} · {scenario}'
        print(label[:71].ljust(72) + ''.join(cells.get(p, '-').ljust(9) for p in platforms))
    doubles = sum(1 for r in records if r['proof'] == 'service-double')
    if doubles:
        print(f'\n{doubles} record(s) were checked against service doubles, not real services.')


if __name__ == '__main__':
    main(sys.argv[1:])
