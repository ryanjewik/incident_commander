$ids = @(17286112,17286113,17286114)
$api = '235e460a182594262066d05bced5ce76'
$app = '15aee144f7d540485cf08a4a3e6ab88f7abd3a53'
$site = 'us5.datadoghq.com'
foreach ($i in $ids) {
    $u = "https://api.$site/api/v1/monitor/$i?api_key=$api&application_key=$app"
    try {
        $r = Invoke-RestMethod -Uri $u -UseBasicParsing -TimeoutSec 15
    } catch {
        Write-Output "--- $i ERROR: $_"
        continue
    }
    Write-Output "--- $i"
    Write-Output "name: $($r.name)"
    Write-Output "overall_state: $($r.overall_state)"
    Write-Output "query: $($r.query)"
    if ($r.tags) { Write-Output "tags: $($r.tags -join ',')" } else { Write-Output "tags: <none>" }
}
