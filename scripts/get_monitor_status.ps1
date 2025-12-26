$ids = @(17286112,17286113,17286114,17286336,17286337,17286338,17286829,17286830,17286831,17286832,17302236)
$api = '235e460a182594262066d05bced5ce76'
$app = '15aee144f7d540485cf08a4a3e6ab88f7abd3a53'
$site = 'us5.datadoghq.com'
$h = @{ 'DD-API-KEY' = $api; 'DD-APPLICATION-KEY' = $app }

foreach ($i in $ids) {
    $u = "https://api.$site/api/v1/monitor/$i"
    try {
        $r = Invoke-RestMethod -Method Get -Uri $u -Headers $h -UseBasicParsing -TimeoutSec 15
    } catch {
        Write-Output "--- $i ERROR: $_"
        continue
    }
    Write-Output "--- Monitor $($r.id)"
    Write-Output "Name: $($r.name)"
    Write-Output "Overall state: $($r.overall_state)"
    if ($r.overall_state_modified) { Write-Output "State changed: $($r.overall_state_modified)" }
    if ($r.query) { Write-Output "Query: $($r.query)" }
    if ($r.tags) { Write-Output "Tags: $($r.tags -join ',')" }
    Write-Output ""
}
