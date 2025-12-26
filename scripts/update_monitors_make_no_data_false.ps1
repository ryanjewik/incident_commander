$ids = @(17286114,17286336,17286337,17286338)
$api = '235e460a182594262066d05bced5ce76'
$app = '15aee144f7d540485cf08a4a3e6ab88f7abd3a53'
$site = 'us5.datadoghq.com'
$h = @{ 'DD-API-KEY' = $api; 'DD-APPLICATION-KEY' = $app; 'Content-Type'='application/json' }

foreach ($i in $ids) {
    try {
        $u = "https://api.$site/api/v1/monitor/$i"
        $r = Invoke-RestMethod -Method Get -Uri $u -Headers $h -UseBasicParsing -TimeoutSec 15
    } catch {
        Write-Output ("ERROR fetching {0}: {1}" -f $i, $_.Exception.Message)
        continue
    }

    # Build a safe update that widens the evaluation window and disables no-data alerts
    $newOptions = $r.options
    $newOptions.notify_no_data = $false

    # If the query uses last_1m, switch to last_5m to give ingestion time
    $q = $r.query -replace 'last_1m','last_5m'

    $body = @{ query = $q; options = $newOptions } | ConvertTo-Json -Depth 10
    try {
        $resp = Invoke-RestMethod -Method Put -Uri $u -Headers $h -Body $body -UseBasicParsing -TimeoutSec 15
        Write-Output ("Updated monitor {0}: notify_no_data={1}" -f $resp.id, $resp.options.notify_no_data)
    } catch {
        Write-Output ("ERROR updating {0}: {1}" -f $i, $_.Exception.Message)
    }
}
