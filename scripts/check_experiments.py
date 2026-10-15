#!/usr/bin/env python3
"""Validate the retained experiment samples and reproducible derived reports."""

import collections
import hashlib
import json
import math
from pathlib import Path
import shutil
import statistics
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / 'docs/experiments/2026-10-15'
HOSTS = ('A', 'B', 'C')
SUITES = {'main': (1908, 3), 'ffiopt': (660, 5), 'extra': (546, 3),
          'kernel': (1008, 3), 'table': (720, 3)}


def read_json(path):
    return json.loads(path.read_text())


def require(condition, message):
    if not condition:
        raise ValueError(message)


def aggregate(directory, suite, expected_count, repeats, keys, metrics):
    groups = collections.defaultdict(lambda: collections.defaultdict(list))
    hashes, sizes = set(), set()
    for host in HOSTS:
        path = directory / 'results' / f'{suite}-{host}.jsonl'
        rows = [json.loads(line) for line in path.read_text().splitlines()]
        require(len(rows) == expected_count, f'{path}: unexpected sample count')
        meta = read_json(path.with_suffix('.meta.json'))
        require(meta['exit'] == 0, f'{path}: nonzero process exit')
        hashes.add(meta['sha256'])
        sizes.add(meta['bytes'])
        for row in rows:
            key = tuple((k, row[k]) for k in keys if k in row)
            require(row['n'] > 0, f'{path}: invalid benchmark iteration count')
            for metric in metrics:
                value = row.get(metric, 0)
                require(math.isfinite(value) and value >= 0,
                        f'{path}: invalid {metric}')
            groups[key][host].append(row)
    require(len(hashes) == len(sizes) == 1,
            f'{suite}: hosts did not execute the same binary')
    out = {}
    for key, hosts in groups.items():
        require(set(hosts) == set(HOSTS), f'{suite}/{key}: missing host')
        medians = {}
        for host, rows in hosts.items():
            require(len(rows) == repeats, f'{suite}/{key}: wrong repetition count')
            require({r['rep'] for r in rows} == set(range(repeats)),
                    f'{suite}/{key}: duplicate/missing repetition')
            medians[host] = {m: statistics.median(r.get(m, 0) for r in rows)
                             for m in metrics}
            if suite != 'scan':
                medians[host]['samples'] = len(rows)
        result = dict(key)
        if suite != 'scan':
            result['suite'] = suite
        result.update({m: statistics.median(v[m] for v in medians.values())
                       for m in metrics})
        result.update(hosts=medians,
                      host_min_ns=min(v['ns'] for v in medians.values()),
                      host_max_ns=max(v['ns'] for v in medians.values()))
        out[key] = result
    return list(out.values())


def compare_summary(directory, expected):
    actual = read_json(directory / 'results/summary.json')
    normalize = lambda rows: sorted(json.dumps(r, sort_keys=True) for r in rows)
    require(normalize(actual) == normalize(expected),
            f'{directory}: summary differs from independently computed medians')


def check_generated(directory, outputs):
    with tempfile.TemporaryDirectory(prefix='pwr-data-check-') as tmp:
        work = Path(tmp)
        shutil.copytree(directory / 'results', work / 'results')
        for script in ('analyze.py', 'write_report.py'):
            subprocess.run([sys.executable, str(directory / script)], cwd=work,
                           check=True, stdout=subprocess.DEVNULL)
        for output in outputs:
            require((work / output).read_bytes() == (directory / output).read_bytes(),
                    f'{directory}/{output}: generated file is stale')


def main():
    provenance = read_json(DATA / 'provenance.json')
    for name, expected_hash in provenance['raw_sample_sha256'].items():
        require(hashlib.sha256((DATA / name).read_bytes()).hexdigest() == expected_hash,
                f'{name}: retained raw samples changed')
    alt = DATA / 'alternatives'
    keys = ('backend', 'fixture', 'stage', 'kind', 'fields', 'fanout',
            'reuse_frame', 'share_decode')
    metrics = ('ns', 'bytes', 'allocs', 'prepare_ns')
    alternatives = []
    for suite, (count, repeats) in SUITES.items():
        alternatives.extend(aggregate(alt, suite, count, repeats, keys, metrics))
    require(len(alternatives) == 1526, 'alternatives: unexpected condition count')
    compare_summary(alt, alternatives)
    inc = DATA / 'incremental'
    incremental = aggregate(inc, 'scan', 1470, 3,
                            ('backend', 'fixture', 'scenario', 'stage'),
                            ('ns', 'bytes', 'allocs'))
    require(len(incremental) == 490, 'incremental: unexpected condition count')
    compare_summary(inc, incremental)
    outputs = ['results/summary.json', 'results/summary.csv', 'report.html', 'REPORT.ko.md']
    check_generated(alt, outputs + ['results/size_summary.json'])
    check_generated(inc, outputs)
    sizes = {r['binary']: r for r in read_json(inc / 'results/sizes.json')}
    require(sizes['scanbench']['bytes'] - sizes['size-base']['bytes'] == 8192,
            'incremental: unexpected retained size delta')
    require(sizes['scanbench']['sha256'] == read_json(inc / 'results/scan-A.meta.json')['sha256'],
            'incremental: size record does not identify the measured binary')
    print('Verified 18,936 raw samples / 2,016 conditions, hashes, medians, and generated reports.')


if __name__ == '__main__':
    main()
