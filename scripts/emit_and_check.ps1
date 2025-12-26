Invoke-RestMethod -Uri 'http://127.0.0.1:8000/emit/pg_failed?count=500' -Method Post | ConvertTo-Json -Depth 4
Write-Host 'emit sent'
Start-Sleep -Seconds 8
$q = [System.Uri]::EscapeDataString('sum:dummy.pg.insert.failed{*}')
Write-Host 'Querying /dd/query...'
Invoke-RestMethod -Uri "http://127.0.0.1:8000/dd/query?q=$q&minutes=15" -Method Get | ConvertTo-Json -Depth 6
Write-Host 'Running scripts/get_monitor_status.ps1'
.\scripts\get_monitor_status.ps1
