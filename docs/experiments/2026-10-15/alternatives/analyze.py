import collections,csv,json,pathlib,statistics
root=pathlib.Path('results')
all_summary=[]
expected={'main':1908,'ffiopt':660,'extra':546,'kernel':1008,'table':720}
for suite,total in expected.items():
    files=[root/f'{suite}-{h}.jsonl' for h in ['A','B','C']]
    if not all(p.exists() for p in files):continue
    groups=collections.defaultdict(list)
    for h,p in zip(['A','B','C'],files):
        rows=[json.loads(line) for line in p.read_text().splitlines()]
        assert len(rows)==total,(p,len(rows),total)
        meta=json.loads(p.with_suffix('.meta.json').read_text())
        assert meta['exit']==0
        for r in rows:
            keys={k:r[k] for k in ['backend','fixture','stage','kind','fields','fanout','reuse_frame','share_decode'] if k in r}
            groups[json.dumps(keys,sort_keys=True),h].append(r)
    merged=collections.defaultdict(dict)
    for (key,h),rows in groups.items():
        merged[key][h]={metric:statistics.median(r.get(metric,0) for r in rows) for metric in ['ns','bytes','allocs','prepare_ns']}
        merged[key][h]['samples']=len(rows)
    for key,hosts in merged.items():
        row={'suite':suite,**json.loads(key)}
        for metric in ['ns','bytes','allocs','prepare_ns']:
            row[metric]=statistics.median(v[metric] for v in hosts.values())
        row.update(host_min_ns=min(v['ns'] for v in hosts.values()),host_max_ns=max(v['ns'] for v in hosts.values()),hosts=hosts)
        all_summary.append(row)
all_summary.sort(key=lambda r:(r['suite'],r['fixture'],r.get('stage',r.get('kind','')),r['fields'],r['ns']))
root.joinpath('summary.json').write_text(json.dumps(all_summary,indent=2)+'\n')
columns=['suite','backend','fixture','stage','kind','fields','fanout','reuse_frame','share_decode','ns','bytes','allocs','prepare_ns','host_min_ns','host_max_ns']
with root.joinpath('summary.csv').open('w') as f:
    writer=csv.DictWriter(f,fieldnames=columns,extrasaction='ignore',lineterminator='\n');writer.writeheader();writer.writerows(all_summary)
print('Aggregated',len(all_summary),'cases from',sum(total*3 for suite,total in expected.items() if (root/f'{suite}-A.jsonl').exists()),'samples')

sizes=json.loads(root.joinpath('sizes.json').read_text())
baseline=next(r['bytes'] for r in sizes if r['tag']=='baseline')
for tag in ['kernel','table']:
    p=pathlib.Path('bin/size-'+tag)
    if p.exists() and not any(r['tag']==tag for r in sizes):sizes.append({'tag':tag,'bytes':p.stat().st_size,'exit':0})
for r in sizes:
    if r['exit']==0:r['delta']=r['bytes']-baseline
root.joinpath('size_summary.json').write_text(json.dumps(sizes,indent=2)+'\n')
defs=[json.loads(l) for l in root.joinpath('backends.jsonl').read_text().splitlines()]
html='''<!doctype html><html lang="ko"><meta charset="utf-8"><title>CEL wire reader benchmark</title>
<style>body{font:15px system-ui;margin:32px;color:#172033}h1{font-size:24px}select,input{padding:7px;margin:4px}table{border-collapse:collapse;width:100%;font-variant-numeric:tabular-nums}th,td{padding:8px;border-bottom:1px solid #dde3ec;text-align:right}th:first-child,td:first-child{text-align:left}th{background:#eef2f7;cursor:pointer;position:sticky;top:0}tr:hover{background:#f3f7fc}.note{color:#526174;max-width:1100px;line-height:1.6}.controls{margin:24px 0}code{background:#f0f3f8;padding:2px}a{color:#135cc5}</style>
<h1>CEL / protobuf wire 읽기 실험</h1><p class="note">Ryzen 5 7640HS 세 대, CPU 2 고정, GOMAXPROCS=1. 각 호스트 중앙값을 구한 뒤 세 값의 중앙값을 표시합니다. 단위는 <b>µs/op</b>이며 fanout/batch 32는 한 op에 32회 전달/메시지를 처리합니다. SQL 권한검사와 전송은 제외했습니다. 첫 일치에서 종료하는 경로는 scalar 중복 없음, alias는 입력 불변을 가정합니다. 자세한 조건과 판정은 REPORT.ko.md를 참고하세요.</p>
<div class="controls"><label>실험 <select id="suite"></select></label><label>입력 <select id="fixture"></select></label><label>단계 <select id="stage"></select></label><label>필드 <select id="fields"><option>1</option><option>4</option><option>0</option></select></label><label>fanout/batch <select id="fanout"><option>1</option><option>32</option><option>0</option></select></label><input id="search" placeholder="방식 이름 검색"></div>
<p id="count" class="note"></p><table><thead><tr><th data-key="backend">방식</th><th data-key="ns">µs/op ↑</th><th data-key="bytes">Go B/op</th><th data-key="allocs">Go alloc/op</th><th>호스트 범위 µs</th><th>frame 재사용</th><th>decode 공유</th><th>중복 scalar</th></tr></thead><tbody></tbody></table>
<p class="note"><a href="results/summary.csv">CSV</a> · <a href="results/summary.json">집계 JSON</a> · 원시 샘플 및 바이너리 SHA256은 results/ 아래에 있습니다. C/Rust는 네 문자열 필드의 제한된 kernel이며 범용 protobuf 런타임 크기와 같지 않습니다. decoded_reuse는 이미 디코딩된 값의 하한 기준입니다.</p>
<script>const data=DATA;const defs=DEFS;const $=id=>document.getElementById(id);let sort='ns',direction=1;
function options(id,values){const el=$(id),old=el.value;el.replaceChildren(...values.map(v=>{const o=document.createElement('option');o.value=v;o.textContent=v;return o}));if(values.includes(old))el.value=old}
options('suite',[...new Set(data.map(r=>r.suite))]);$('suite').value='main';
function updateChoices(){let rs=data.filter(r=>r.suite===$('suite').value);options('fixture',[...new Set(rs.map(r=>r.fixture))]);rs=rs.filter(r=>r.fixture===$('fixture').value);options('stage',[...new Set(rs.map(r=>r.stage||r.kind))]);rs=rs.filter(r=>(r.stage||r.kind)===$('stage').value);options('fields',[...new Set(rs.map(r=>String(r.fields)))].sort());rs=rs.filter(r=>r.fields==+$('fields').value);options('fanout',[...new Set(rs.map(r=>String(r.fanout??1)))].sort());render()}
function render(){const rows=data.filter(r=>r.suite===$('suite').value&&r.fixture===$('fixture').value&&(r.stage||r.kind)===$('stage').value&&r.fields==+$('fields').value&&(r.fanout===undefined||r.fanout==+$('fanout').value)&&r.backend.includes($('search').value));rows.sort((a,b)=>direction*(typeof a[sort]==='string'?a[sort].localeCompare(b[sort]):a[sort]-b[sort]));$('count').textContent=rows.length+'개 결과';document.querySelector('tbody').replaceChildren(...rows.map(r=>{const tr=document.createElement('tr');const d=defs.find(x=>x.backend===r.backend);const values=[r.backend,(r.ns/1000).toFixed(3),r.bytes,r.allocs,(r.host_min_ns/1000).toFixed(3)+'–'+(r.host_max_ns/1000).toFixed(3),r.reuse_frame??'—',r.share_decode??'—',d?(d.first_wins?'첫 값 (입력 가정)':'마지막 값'):'—'];for(const v of values){const td=document.createElement('td');td.textContent=v;tr.append(td)}return tr}))}
$('suite').onchange=updateChoices;for(const id of ['fixture','stage','fields','fanout'])$(id).oninput=updateChoices;$('search').oninput=render;for(const th of document.querySelectorAll('th[data-key]'))th.onclick=()=>{if(sort===th.dataset.key)direction*=-1;else{sort=th.dataset.key;direction=1}render()};updateChoices();
</script></html>'''
pathlib.Path('report.html').write_text(html.replace('DATA',json.dumps(all_summary,separators=(',',':'))).replace('DEFS',json.dumps(defs,separators=(',',':'))))
