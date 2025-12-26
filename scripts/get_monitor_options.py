#!/usr/bin/env python3
import requests, json
api='235e460a182594262066d05bced5ce76'
app='15aee144f7d540485cf08a4a3e6ab88f7abd3a53'
site='us5.datadoghq.com'
headers={'DD-API-KEY':api,'DD-APPLICATION-KEY':app}
url=f'https://api.{site}/api/v1/monitor/17286831'
resp=requests.get(url, headers=headers, timeout=15)
resp.raise_for_status()
print(json.dumps(resp.json().get('options', {}), indent=2))
