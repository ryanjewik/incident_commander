import os,requests,json
DD_API=os.environ.get('DD_API_KEY')
DD_APP=os.environ.get('DD_APP_KEY')
DD_SITE=os.environ.get('DD_SITE','datadoghq.com')
base=f"https://api.{DD_SITE}/api/v1"
headers={"DD-API-KEY":DD_API,"DD-APPLICATION-KEY":DD_APP,"Content-Type":"application/json"}
mon={
  "name":"Test create pg.log_errors monitor",
  "type":"metric alert",
  "query":"sum(last_5m):sum:pg.log_errors{service:postgres,env:demo} > 10",
  "message":"test",
  "tags":["service:postgres","env:demo"],
  "options":{"notify_audit":False,"locked":False,"timeout_h":0,"notify_no_data":False}
}
resp=requests.post(base+"/monitor",headers=headers,data=json.dumps(mon))
print(resp.status_code)
print(resp.text)
