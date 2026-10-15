import collections,csv,json,pathlib,statistics
root=pathlib.Path('results')
groups=collections.defaultdict(lambda:collections.defaultdict(list))
for host in ['A','B','C']:
    rows=[json.loads(l) for l in (root/f'scan-{host}.jsonl').read_text().splitlines()]
    assert len(rows)==1470,(host,len(rows))
    meta=json.loads((root/f'scan-{host}.meta.json').read_text())
    assert meta['exit']==0
    for r in rows:groups[tuple(r[k] for k in ['backend','fixture','scenario','stage'])][host].append(r)
assert len(groups)==490,len(groups)
out=[]
for (backend,fixture,scenario,stage),hosts in groups.items():
    assert set(hosts)=={'A','B','C'}
    assert all(len(rows)==3 for rows in hosts.values())
    medians={h:{k:statistics.median(r[k] for r in rows) for k in ['ns','bytes','allocs']} for h,rows in hosts.items()}
    row=dict(backend=backend,fixture=fixture,scenario=scenario,stage=stage,hosts=medians)
    row.update({k:statistics.median(v[k] for v in medians.values()) for k in ['ns','bytes','allocs']})
    row.update(host_min_ns=min(v['ns'] for v in medians.values()),host_max_ns=max(v['ns'] for v in medians.values()))
    out.append(row)
out.sort(key=lambda r:(r['fixture'],r['scenario'],r['stage'],r['ns']))
(root/'summary.json').write_text(json.dumps(out,indent=2)+'\n')
columns=['backend','fixture','scenario','stage','ns','bytes','allocs','host_min_ns','host_max_ns']
with (root/'summary.csv').open('w') as f:
    w=csv.DictWriter(f,fieldnames=columns,extrasaction='ignore',lineterminator='\n');w.writeheader();w.writerows(out)
html='''<!doctype html><html lang="ko"><meta charset="utf-8"><title>점진적 wire 스캔 비교</title>
<style>body{font:15px system-ui;margin:30px;color:#172033}p{max-width:1100px;line-height:1.6}select{padding:7px;margin:5px}table{border-collapse:collapse;width:100%;font-variant-numeric:tabular-nums}th,td{padding:9px;border-bottom:1px solid #ddd;text-align:right}th:first-child,td:first-child{text-align:left}th{background:#eef2f7;cursor:pointer}a{color:#135cc5}</style>
<h1>점진적 protobuf wire 스캔</h1><p>Ryzen 5 7640HS 세 대, CPU2 고정, GOMAXPROCS=1, 100ms×3회. 각 호스트 중앙값의 중앙값. 단위는 µs/event이며 session/index 생성 비용을 포함합니다. SQL·권한검사·전송은 제외했습니다. first는 중복 scalar 없음, last는 마지막 scalar 값을 채택하며 전체 strict validation은 아닙니다.</p>
<label>입력<select id="fixture"></select></label><label>접근 패턴<select id="scenario"></select></label><label>단계<select id="stage"></select></label>
<table><thead><tr><th data-key="backend">방식</th><th data-key="ns">µs/event</th><th data-key="bytes">Go B/op</th><th data-key="allocs">Go alloc/op</th><th>호스트 범위 µs</th></tr></thead><tbody></tbody></table>
<p>wire: 각 접근마다 처음부터 검색. descriptor_table: 관심 필드를 미리 받아 한 번 스캔. incremental_index: 요청 시 스캔을 진행하고 알려진 필드 위치 및 EOF를 캐시. go_generated_unmarshal: 이벤트마다 전체 decode 1회.<br>map256_prefix는 관심 필드 네 개 앞에 map을, map256_late는 artifact_type 앞에 map을 둔 입력입니다. metadata_false는 payload 접근 없이 envelope에서 거부하는 경우입니다.</p>
<p><a href="REPORT.ko.md">보고서</a> · <a href="results/summary.csv">CSV</a> · <a href="results/summary.json">JSON</a></p>
<script>const data=DATA;const $=id=>document.getElementById(id);let key='ns',direction=1;
for(const id of ['fixture','scenario','stage']){for(const v of [...new Set(data.map(r=>r[id]))]){const o=document.createElement('option');o.value=v;o.textContent=v;$(id).append(o)}$(id).onchange=render}
$('fixture').value='map256_prefix';$('scenario').value='sequence_2_1_3';$('stage').value='direct';
function render(){const rows=data.filter(r=>['fixture','scenario','stage'].every(id=>r[id]===$(id).value));rows.sort((a,b)=>direction*(typeof a[key]==='string'?a[key].localeCompare(b[key]):a[key]-b[key]));document.querySelector('tbody').replaceChildren(...rows.map(r=>{const tr=document.createElement('tr');for(const v of [r.backend,(r.ns/1000).toFixed(3),r.bytes,r.allocs,(r.host_min_ns/1000).toFixed(3)+'–'+(r.host_max_ns/1000).toFixed(3)]){const td=document.createElement('td');td.textContent=v;tr.append(td)}return tr}))}
for(const th of document.querySelectorAll('th[data-key]'))th.onclick=()=>{if(key===th.dataset.key)direction*=-1;else{key=th.dataset.key;direction=1}render()};render();</script></html>'''
pathlib.Path('report.html').write_text(html.replace('DATA',json.dumps(out,separators=(',',':'))))
print('Aggregated 4410 samples /490 conditions')
