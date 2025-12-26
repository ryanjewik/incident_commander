Write-Output 'Retrying smoke tests with retries...'
$tests = @(
    @{ method = 'Get'; uri = 'http://localhost:8000/health' },
    @{ method = 'Post'; uri = 'http://localhost:8000/pg/insert?name=ci-test-2' },
    @{ method = 'Get'; uri = 'http://localhost:8000/pg/query?limit=3' },
    @{ method = 'Post'; uri = 'http://localhost:8000/pg/alert' },
    @{ method = 'Post'; uri = 'http://localhost:8000/dd/emit_gauge_fallback?name=dummy.gemini.duration_ms&value=25000&count=200&tags=service:gemini,env:demo' },
    @{ method = 'Get'; uri = 'http://localhost:8000/dd/query?q=avg:dummy.gemini.duration_ms{service:gemini,env:demo}&minutes=15' }
)
$results = @()
foreach ($u in $tests) {
    $ok = $false
    for ($i = 0; $i -lt 8; $i++) {
        try {
            $r = Invoke-RestMethod -Method $u.method -Uri $u.uri -TimeoutSec 20
            $results += [ordered]@{ endpoint = $u.uri; method = $u.method; result = $r; attempt = ($i + 1) }
            $ok = $true
            break
        } catch {
            Start-Sleep -Seconds 3
        }
    }
    if (-not $ok) {
        $results += [ordered]@{ endpoint = $u; error = 'unreachable after retries' }
    }
}
Write-Output 'SMOKE TEST RESULTS:'
$results | ConvertTo-Json -Depth 6
