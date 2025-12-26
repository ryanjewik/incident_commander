import os, time, urllib.request, urllib.parse, json

def load_env(path='.env'):
    d={}
    with open(path) as f:
        for line in f:
            line=line.strip()
            if not line or line.startswith('#'): continue
            if '=' not in line: continue
            k,v=line.split('=',1); d[k.strip()]=v.strip().strip('"').strip("'")
    return d

env=load_env(os.path.join(os.path.dirname(__file__), '..', '.env'))
api=env.get('DD_API_KEY')
app=env.get('DD_APP_KEY')
site=env.get('DD_SITE','datadoghq.com')
if not api or not app:
    print('Missing DD keys in .env'); raise SystemExit(1)
now=int(time.time())
frm=now-600

def query(q):
    url=f"https://api.{site}/api/v1/query?from={frm}&to={now}&query={urllib.parse.quote(q,safe='') }"
    req=urllib.request.Request(url, headers={'DD-API-KEY':api,'DD-APPLICATION-KEY':app})
    with urllib.request.urlopen(req, timeout=30) as r:
        data=r.read().decode()
        try:
            j=json.loads(data)
            print(json.dumps(j, indent=2))
        except Exception:
            print(data)

print('---- GEMINI ----')
query('avg:dummy.gemini.duration_ms{*}')
print('\n---- PG.LOG_ERRORS ----')
query('sum:pg.log_errors{service:postgres,env:demo}')
