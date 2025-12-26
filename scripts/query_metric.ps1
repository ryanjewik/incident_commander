$envFile = Join-Path (Get-Location) '.env'
if (-Not (Test-Path $envFile)) { Write-Error ".env not found"; exit 2 }
Get-Content $envFile | ForEach-Object {
  if ($_ -match '^\s*#' -or $_ -match '^\s*$') { } elseif ($_ -match '^([^=]+)=(.*)$') {
    $name = $matches[1].Trim()
    $val = $matches[2].Trim()
    Set-Item -Path env:$name -Value $val
  }
}
$dd_site = $env:DD_SITE
$dd_api = $env:DD_API_KEY
$dd_app = $env:DD_APP_KEY
if (-not $dd_site -or -not $dd_api -or -not $dd_app) { Write-Error 'Missing DD_SITE or keys in .env'; exit 2 }
$to = [int][double]::Parse((Get-Date -UFormat %s))
$from = $to - 900
$q = [System.Uri]::EscapeDataString('avg:dummy.gemini.duration_ms{*}')
$url = "https://$dd_site/api/v1/query?from=$from&to=$to&query=$q"
Write-Output "Querying: $url"
try {
  $resp = Invoke-RestMethod -Method Get -Uri $url -Headers @{ 'DD-API-KEY' = $dd_api; 'DD-APPLICATION-KEY' = $dd_app } -ErrorAction Stop
  $resp | ConvertTo-Json -Depth 6
} catch {
  Write-Error $_.Exception.Message
  exit 3
}
