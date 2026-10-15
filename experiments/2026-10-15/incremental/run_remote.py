import hashlib,json,os,pathlib,platform,resource,subprocess,sys,time
root=pathlib.Path(__file__).resolve().parent
os.chdir(root)
label=sys.argv[1] if len(sys.argv)>1 else 'main'
binary=sys.argv[2] if len(sys.argv)>2 else 'bench'
extra=sys.argv[3:]
meta={'host':platform.node(),'start':time.time(),'binary':binary,'sha256':hashlib.sha256(pathlib.Path(binary).read_bytes()).hexdigest(),'bytes':pathlib.Path(binary).stat().st_size,'load_before':pathlib.Path('/proc/loadavg').read_text().strip()}
cmd=['taskset','-c','2','./'+binary,'-duration','100ms','-count','3','-seed',str(sum(map(ord,platform.node())))]+extra
meta['command']=cmd
with open(label+'.jsonl','w') as output,open(label+'.log','w') as errors:
    p=subprocess.run(cmd,stdout=output,stderr=errors,env={**os.environ,'GOMAXPROCS':'1'})
meta.update(exit=p.returncode,elapsed=time.time()-meta['start'],load_after=pathlib.Path('/proc/loadavg').read_text().strip(),max_rss_kib=resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss)
pathlib.Path(label+'.meta.json').write_text(json.dumps(meta,indent=2)+'\n')
print(json.dumps(meta),flush=True)
sys.exit(p.returncode)
