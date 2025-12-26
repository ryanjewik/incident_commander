$api = '235e460a182594262066d05bced5ce76'
$app = '15aee144f7d540485cf08a4a3e6ab88f7abd3a53'
$site = 'us5.datadoghq.com'
$headers = @{ 'DD-API-KEY' = $api; 'DD-APPLICATION-KEY' = $app; 'Content-Type' = 'application/json' }

$monitors = @(
    @{ name = 'Simulated Dummy Gemini high latency'; type = 'metric alert'; query = 'avg(last_1m):avg:dummy.gemini.duration_ms{service:gemini,env:demo} > 10000'; message = 'Triggered by dummy app (simulated)'; tags = @('service:gemini','env:demo'); options = @{ notify_no_data = $false; thresholds = @{ critical = 10000 } } },
    @{ name = 'Simulated Dummy system CPU high'; type = 'metric alert'; query = 'avg(last_1m):avg:dummy.system.cpu_percent{env:demo} > 10'; message = 'Simulated CPU high'; tags = @('env:demo'); options = @{ notify_no_data = $false; thresholds = @{ critical = 10 } } },
    @{ name = 'Simulated Dummy system memory high'; type = 'metric alert'; query = 'avg(last_1m):avg:dummy.system.memory_percent{env:demo} > 30'; message = 'Simulated memory high'; tags = @('env:demo'); options = @{ notify_no_data = $false; thresholds = @{ critical = 30 } } }
)

foreach ($m in $monitors) {
    $body = $m | ConvertTo-Json -Depth 10
    try {
        $resp = Invoke-RestMethod -Method Post -Uri "https://api.$site/api/v1/monitor" -Headers $headers -Body $body -UseBasicParsing -TimeoutSec 30
        Write-Output "Created monitor: $($resp.id) $($resp.name)"
    } catch {
        Write-Output "ERROR creating monitor: $_"
    }
}
