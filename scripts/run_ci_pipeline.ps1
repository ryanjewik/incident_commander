# CI pipeline: rebuild compose, create monitors, run integration smoke tests
Write-Output 'Loading .env into environment'
$envFile = Join-Path (Get-Location) '.env'
if (-Not (Test-Path $envFile)) { Write-Error '.env not found'; exit 2 }
Get-Content $envFile | ForEach-Object {
    if ($_ -match '^\s*#' -or $_ -match '^\s*$') { } elseif ($_ -match '^([^=]+)=(.*)$') {
        $name = $matches[1].Trim()
        $val = $matches[2].Trim()
        Set-Item -Path env:$name -Value $val
    }
}

Write-Output 'Rebuilding and starting compose stack (this may take a while)...'
docker compose -f docker-compose.dummy.yml up -d --build
if ($LASTEXITCODE -ne 0) { Write-Error 'docker compose up failed'; exit 2 }

Write-Output 'Waiting 20s for containers to initialize...'
Start-Sleep -Seconds 20

Write-Output 'Running Datadog monitor creation scripts...'
try { python scripts\create_datadog_monitors.py } catch { Write-Error "create_datadog_monitors.py failed: $_" }
try { python scripts\create_postgres_monitors.py } catch { Write-Error "create_postgres_monitors.py failed: $_" }

Start-Sleep -Seconds 5
Write-Output 'Running integration smoke tests against dummy app...'
$results = @()

function safeInvoke($method, $uri) {
    try {
        $r = Invoke-RestMethod -Method $method -Uri $uri -TimeoutSec 120
        return @{ ok = $true; resp = $r }
    } catch {
        return @{ ok = $false; err = $_.Exception.Message }
    }
}

# Test sequence
$tests = @(
    @{ method = 'Post'; uri = 'http://localhost:8000/pg/insert?name=ci-test-1' },
    @{ method = 'Get'; uri = 'http://localhost:8000/pg/query?limit=5' },
    @{ method = 'Post'; uri = 'http://localhost:8000/pg/alert' },
    @{ method = 'Post'; uri = 'http://localhost:8000/dd/emit_gauge_fallback?name=dummy.gemini.duration_ms&value=25000&count=500&tags=service:gemini,env:demo' },
    @{ method = 'Get'; uri = 'http://localhost:8000/dd/query?q=avg:dummy.gemini.duration_ms{service:gemini,env:demo}&minutes=15' }
)

foreach ($t in $tests) {
    Write-Output "Testing $($t.uri)"
    $res = safeInvoke $t.method $t.uri
    $entry = [ordered]@{ endpoint = $t.uri }
    if ($res.ok) { $entry.Add('result', $res.resp) } else { $entry.Add('error', $res.err) }
    $results += (New-Object PSObject -Property $entry)
    Start-Sleep -Seconds 2
}

Write-Output 'SMOKE TEST RESULTS:'
$results | ConvertTo-Json -Depth 6

Write-Output 'Polling Datadog monitor statuses...'
& powershell -NoProfile -ExecutionPolicy Bypass -File scripts\get_monitor_status.ps1
