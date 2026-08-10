param(
    [string]$Image = "nekonest-server:smoke",
    [string]$Version = "0.0.0-ci",
    [switch]$SkipBuild
)

$ErrorActionPreference = "Stop"
$suffix = [Guid]::NewGuid().ToString("N").Substring(0, 12)
$volume = "nekonest-smoke-$suffix"
$firstContainer = "nekonest-smoke-a-$suffix"
$secondContainer = "nekonest-smoke-b-$suffix"
$adminSecret = "admin-sentinel-$suffix"
$bootstrapToken = "bootstrap-sentinel-$suffix"
$deviceID = "device_docker_$suffix"
$deviceName = "prompt-sentinel-$suffix"

function Invoke-Docker {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)
    $output = @(& docker @Arguments 2>&1 | ForEach-Object { $_.ToString() })
    if ($LASTEXITCODE -ne 0) {
        throw "docker $($Arguments -join ' ') failed:`n$($output -join [Environment]::NewLine)"
    }
    return $output
}

function Assert-DockerFails {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)
    $output = @(& docker @Arguments 2>&1 | ForEach-Object { $_.ToString() })
    if ($LASTEXITCODE -eq 0) {
        throw "docker $($Arguments -join ' ') unexpectedly succeeded:`n$($output -join [Environment]::NewLine)"
    }
}

function Start-SmokeContainer {
    param([string]$Name)
    $arguments = @(
        "run", "--detach", "--name", $Name,
        "--read-only", "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=64m,uid=10001,gid=10001,mode=1770",
        "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
        "--publish", "127.0.0.1::8080",
        "--mount", "type=volume,src=$volume,dst=/data",
        "--env", "NEKONEST_ADMIN_SECRET=$adminSecret",
        "--env", "NEKONEST_BOOTSTRAP_TOKEN=$bootstrapToken",
        "--env", "NEKONEST_ALLOWED_ORIGINS=http://127.0.0.1",
        "--env", "NEKONEST_LOG_FORMAT=json",
        "--env", "NEKONEST_LOG_LEVEL=info",
        $Image
    )
    Invoke-Docker @arguments | Out-Null
}

function Wait-Healthy {
    param([string]$Name)
    for ($attempt = 1; $attempt -le 45; $attempt++) {
        $status = (Invoke-Docker "inspect" "--format" "{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}" $Name).Trim()
        if ($status -eq "healthy") {
            return
        }
        if ($status -eq "unhealthy") {
            throw "container $Name became unhealthy"
        }
        Start-Sleep -Seconds 1
    }
    throw "container $Name did not become healthy"
}

function Get-BaseUrl {
    param([string]$Name)
    $mapping = (Invoke-Docker "port" $Name "8080/tcp" | Select-Object -First 1).Trim()
    if ($mapping -notmatch ':(?<port>\d+)$') {
        throw "unexpected port mapping: $mapping"
    }
    return "http://127.0.0.1:$($Matches.port)"
}

function Assert-JSONLogs {
    param([string]$Name)
    $lines = @(Invoke-Docker "logs" $Name | Where-Object { $_.Trim() -ne "" })
    if ($lines.Count -eq 0) {
        throw "container emitted no logs"
    }
    foreach ($line in $lines) {
        $record = $line | ConvertFrom-Json
        foreach ($field in @("time", "level", "msg", "component", "event")) {
            if ($null -eq $record.$field) {
                throw "log record is missing $field`: $line"
            }
        }
    }
    $joined = $lines -join "`n"
    foreach ($sentinel in @($adminSecret, $bootstrapToken, $deviceName)) {
        if ($joined.Contains($sentinel)) {
            throw "sensitive sentinel leaked into logs"
        }
    }
}

function Assert-PrivateDataPermissions {
    param([string]$Name)
    foreach ($expected in @{
            "/data" = "700:10001:10001"
            "/data/nekonest.db" = "600:10001:10001"
            "/data/nekonest.db-wal" = "600:10001:10001"
            "/data/nekonest.db-shm" = "600:10001:10001"
        }.GetEnumerator()) {
        $actual = (Invoke-Docker "exec" $Name "stat" "-c" "%a:%u:%g" $expected.Key).Trim()
        if ($actual -ne $expected.Value) {
            throw "$($expected.Key) permissions=$actual want=$($expected.Value)"
        }
    }
}

try {
    if (-not $SkipBuild) {
        & docker build --progress plain --build-arg "VERSION=$Version" --tag $Image .
        if ($LASTEXITCODE -ne 0) {
            throw "docker build failed"
        }
    }
    Invoke-Docker "volume" "create" $volume | Out-Null

    Start-SmokeContainer $firstContainer
    Wait-Healthy $firstContainer
    Assert-PrivateDataPermissions $firstContainer
    $baseUrl = Get-BaseUrl $firstContainer
    $health = Invoke-RestMethod "$baseUrl/health"
    if ($health.status -ne "nyan~" -or $health.server_version -ne $Version) {
        throw "unexpected health payload: $($health | ConvertTo-Json -Compress)"
    }
    $index = Invoke-WebRequest "$baseUrl/"
    if ($index.StatusCode -ne 200 -or
        -not $index.Content.Contains('<div id="app">') -or
        -not $index.Content.Contains('manifest.webmanifest')) {
        throw "PWA index was not served"
    }
    $assetMatch = [regex]::Match($index.Content, '(?:src|href)="(?<path>/assets/[^"]+)"')
    if (-not $assetMatch.Success) {
        throw "PWA index did not reference a built asset"
    }
    $asset = Invoke-WebRequest "$baseUrl$($assetMatch.Groups['path'].Value)"
    if ($asset.StatusCode -ne 200 -or $asset.RawContentLength -le 0) {
        throw "PWA built asset was not served"
    }

    $inspect = Invoke-Docker "inspect" $firstContainer | ConvertFrom-Json
    if ($inspect[0].Config.User -notmatch '^10001(?::10001)?$') {
        throw "container is not configured as uid 10001"
    }
    if (-not $inspect[0].HostConfig.ReadonlyRootfs) {
        throw "container root filesystem is not read-only"
    }
    if ($inspect[0].HostConfig.CapDrop -notcontains "ALL") {
        throw "container capabilities were not fully dropped"
    }
    if ($inspect[0].HostConfig.SecurityOpt -notcontains "no-new-privileges:true") {
        throw "container no-new-privileges was not enabled"
    }
    Invoke-Docker "exec" $firstContainer "sh" "-c" "touch /data/.smoke-write && rm /data/.smoke-write" | Out-Null
    Assert-DockerFails "exec" $firstContainer "sh" "-c" "touch /app/.rootfs-write-must-fail"

    $registerHeaders = @{ "X-Neko-Bootstrap" = $bootstrapToken }
    $registerBody = @{ device_id = $deviceID; name = $deviceName; os = "linux"; transport_mode = "sealed" } | ConvertTo-Json
    Invoke-RestMethod "$baseUrl/api/devices/register" -Method Post -Headers $registerHeaders -ContentType "application/json" -Body $registerBody | Out-Null
    Assert-JSONLogs $firstContainer

    Invoke-Docker "stop" "--time" "15" $firstContainer | Out-Null
    $exitCode = [int](Invoke-Docker "inspect" "--format" "{{.State.ExitCode}}" $firstContainer).Trim()
    if ($exitCode -ne 0) {
        throw "container did not stop cleanly: exit=$exitCode"
    }
    Invoke-Docker "rm" $firstContainer | Out-Null

    Invoke-Docker "run" "--rm" "--user" "0:0" "--entrypoint" "sh" `
        "--mount" "type=volume,src=$volume,dst=/data" $Image `
        "-c" "chmod 755 /data && chmod 644 /data/nekonest.db*" | Out-Null

    Start-SmokeContainer $secondContainer
    Wait-Healthy $secondContainer
    Assert-PrivateDataPermissions $secondContainer
    $baseUrl = Get-BaseUrl $secondContainer
    $devices = Invoke-RestMethod "$baseUrl/api/devices" -Headers @{ Authorization = "Bearer $adminSecret" }
    if ($devices.devices.id -notcontains $deviceID) {
        throw "registered device did not survive container restart"
    }
    Assert-JSONLogs $secondContainer
    Write-Host "Docker smoke passed for $Image"
}
finally {
    & docker rm --force $firstContainer $secondContainer 2>$null | Out-Null
    & docker volume rm --force $volume 2>$null | Out-Null
}
