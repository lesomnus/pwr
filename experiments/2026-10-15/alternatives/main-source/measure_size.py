import hashlib,json,pathlib,subprocess,time
rows=[]
for tag in ['','wire','gencode','csproto','hyperpb','molecule','dynamicgo','cffi','rustffi','all']:
    name=tag or 'baseline'
    dest=pathlib.Path('bin/size-'+name)
    cmd=['go','build','-trimpath','-ldflags=-s -w','-o',str(dest)]
    if tag:cmd+=['-tags',tag]
    cmd+=['./cmd/bench']
    start=time.time()
    p=subprocess.run(cmd,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
    row={'tag':name,'exit':p.returncode,'build_seconds':time.time()-start}
    if p.returncode==0:
        row.update(bytes=dest.stat().st_size,sha256=hashlib.sha256(dest.read_bytes()).hexdigest())
    else:row['error']=p.stdout
    rows.append(row)
    print(json.dumps(row),flush=True)
pathlib.Path('results/sizes.json').write_text(json.dumps(rows,indent=2)+'\n')
