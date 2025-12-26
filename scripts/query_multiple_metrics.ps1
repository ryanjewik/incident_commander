# Queries multiple Datadog metrics using credentials in .env
$envFile = Join-Path (Get-Location) '.env'
if (-Not (Test-Path $envFile)) { Write-Error ".env not found"; exit 2 }
Get-Content $envFile | ForEach-Object {
  if ($_ -match '^\s*#' -or $_ -match '^\s*$') { } elseif ($_ -match '^([^=]+)=(.*)$') {
    $name = $matches[1].Trim(); $val = $matches[2].Trim(); Set-Item -Path env:$name -Value $val
  }
}
$dd_site = $env:DD_SITE; $dd_api = $env:DD_API_KEY; $dd_app = $env:DD_APP_KEY
if (-not $dd_site -or -not $dd_api -or -not $dd_app) { Write-Error 'Missing DD_SITE or keys in .env'; exit 2 }
$to = [int][double]::Parse((Get-Date -UFormat %s)); $from = $to - 3600
$queries = @(
  'avg:dummy.gemini.duration_ms{*}',
  'sum:dummy.gemini.error{*}',
  'avg:dummy.system.cpu_percent{env:demo}',
  'avg:dummy.system.memory_percent{env:demo}',
  'avg:redis.used_memory_bytes{service:redis}'
)
foreach ($q in $queries) {
  $qe = [System.Uri]::EscapeDataString($q)
  $url = "https://$dd_site/api/v1/query?from=$from&to=$to&query=$qe"
  Write-Output "--- QUERY: $q"
  Write-Output "$url"
  try {
    $resp = Invoke-RestMethod -Method Get -Uri $url -Headers @{ 'DD-API-KEY' = $dd_api; 'DD-APPLICATION-KEY' = $dd_app } -ErrorAction Stop
    $resp | ConvertTo-Json -Depth 6
  } catch {
    Write-Error $_.Exception.Message
  }
  Start-Sleep -Milliseconds 500
}
