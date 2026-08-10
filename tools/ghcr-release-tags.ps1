[CmdletBinding()]
param(
    [string]$Mode = "",
    [string]$Image = "",
    [string]$ReleaseTag = "",
    [string]$Version = "",
    [string]$SourceSha = "",
    [string]$MajorMinor = "",
    [string]$ExpectedMajorMinorDigest = "",
    [string]$ExpectedLatestDigest = "",
    [int]$InspectAttempts = 3,
    [int]$InspectDelaySeconds = 2
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Invoke-CapturedProcess {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [Parameter(Mandatory = $true)][string[]]$ArgumentList
    )

    $startInfo = [System.Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $FilePath
    $startInfo.UseShellExecute = $false
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    foreach ($argument in $ArgumentList) {
        [void]$startInfo.ArgumentList.Add($argument)
    }

    $process = [System.Diagnostics.Process]::new()
    $process.StartInfo = $startInfo
    try {
        [void]$process.Start()
        $stdoutTask = $process.StandardOutput.ReadToEndAsync()
        $stderrTask = $process.StandardError.ReadToEndAsync()
        $process.WaitForExit()
        return [pscustomobject]@{
            ExitCode = $process.ExitCode
            StdOut = $stdoutTask.GetAwaiter().GetResult()
            StdErr = $stderrTask.GetAwaiter().GetResult()
        }
    }
    finally {
        $process.Dispose()
    }
}

function Invoke-RegistryInspectOnce {
    param([Parameter(Mandatory = $true)][string]$Reference)

    return Invoke-CapturedProcess -FilePath "docker" -ArgumentList @(
        "buildx", "imagetools", "inspect", "--format", "{{json .Manifest}}", $Reference
    )
}

function Test-ConfirmedManifestMissing {
    param([AllowEmptyString()][string]$Diagnostic)

    if ($Diagnostic -match '(?i)unauthori[sz]ed|authentication|access denied|forbidden|too many requests|\b429\b|timeout|timed out|connection|temporary|service unavailable|\b5\d\d\b') {
        return $false
    }
    return $Diagnostic -match '(?im)manifest unknown|\b404\s+not found\b|(^|:\s*)not found\s*$'
}

function Get-RegistryManifest {
    param(
        [Parameter(Mandatory = $true)][string]$Reference,
        [int]$Attempts = 3,
        [int]$DelaySeconds = 2,
        [scriptblock]$InspectRunner = $null,
        [scriptblock]$SleepAction = $null
    )

    if ($Attempts -lt 1) {
        throw "registry inspect attempts must be positive"
    }
    if ($null -eq $InspectRunner) {
        $InspectRunner = { param($Ref) Invoke-RegistryInspectOnce -Reference $Ref }
    }
    if ($null -eq $SleepAction) {
        $SleepAction = { param($Seconds) Start-Sleep -Seconds $Seconds }
    }

    $allFailuresConfirmedMissing = $true
    for ($attempt = 1; $attempt -le $Attempts; $attempt++) {
        try {
            $result = & $InspectRunner $Reference
        }
        catch {
            $result = [pscustomobject]@{ ExitCode = -1; StdOut = ""; StdErr = "process failure" }
        }

        $stdout = [string]$result.StdOut
        $stderr = [string]$result.StdErr
        if ([int]$result.ExitCode -eq 0 -and -not [string]::IsNullOrWhiteSpace($stdout)) {
            return [pscustomobject]@{ State = "present"; Raw = $stdout }
        }

        $diagnostic = "$stderr`n$stdout"
        if ([int]$result.ExitCode -eq 0 -or -not (Test-ConfirmedManifestMissing -Diagnostic $diagnostic)) {
            $allFailuresConfirmedMissing = $false
        }
        if ($attempt -lt $Attempts) {
            & $SleepAction $DelaySeconds | Out-Null
        }
    }

    if ($allFailuresConfirmedMissing) {
        return [pscustomobject]@{ State = "missing"; Raw = "" }
    }
    throw "registry inspection failed closed for $Reference after $Attempts attempts"
}

function Get-ManifestDigest {
    param([Parameter(Mandatory = $true)][string]$Raw)

    try {
        $manifest = $Raw | ConvertFrom-Json -AsHashtable
    }
    catch {
        throw "registry returned invalid manifest JSON"
    }
    $digest = [string]$manifest["digest"]
    if ($digest -notmatch '^sha256:[0-9a-f]{64}$') {
        throw "registry manifest digest is missing or invalid"
    }
    return $digest
}

function Get-ManifestAnnotation {
    param(
        [Parameter(Mandatory = $true)][string]$Raw,
        [Parameter(Mandatory = $true)][string]$Name
    )

    try {
        $manifest = $Raw | ConvertFrom-Json -AsHashtable
    }
    catch {
        throw "registry returned invalid manifest JSON"
    }
    if (-not $manifest.ContainsKey("annotations") -or $null -eq $manifest["annotations"]) {
        return ""
    }
    $value = $manifest["annotations"][$Name]
    if ($null -eq $value) {
        return ""
    }
    return [string]$value
}

function Invoke-ExactTagPreflight {
    param(
        [Parameter(Mandatory = $true)][string]$Image,
        [Parameter(Mandatory = $true)][string]$ReleaseTag,
        [Parameter(Mandatory = $true)][string]$Version,
        [Parameter(Mandatory = $true)][string]$SourceSha,
        [int]$Attempts = 3,
        [int]$DelaySeconds = 2,
        [scriptblock]$InspectRunner = $null,
        [scriptblock]$SleepAction = $null
    )

    $presentCount = 0
    $manifestDigest = ""
    $sourceRef = ""
    $missing = [System.Collections.Generic.List[string]]::new()
    foreach ($exactTag in @($ReleaseTag, $Version)) {
        $reference = "${Image}:$exactTag"
        $inspection = Get-RegistryManifest -Reference $reference -Attempts $Attempts -DelaySeconds $DelaySeconds -InspectRunner $InspectRunner -SleepAction $SleepAction
        if ($inspection.State -eq "missing") {
            $missing.Add($exactTag)
            continue
        }

        $revision = Get-ManifestAnnotation -Raw $inspection.Raw -Name "org.opencontainers.image.revision"
        $imageVersion = Get-ManifestAnnotation -Raw $inspection.Raw -Name "org.opencontainers.image.version"
        if ($revision -ne $SourceSha -or $imageVersion -ne $Version) {
            throw "immutable tag $reference has unexpected source annotations"
        }
        $currentDigest = Get-ManifestDigest -Raw $inspection.Raw
        if ($manifestDigest -ne "" -and $manifestDigest -ne $currentDigest) {
            throw "immutable exact tags for $ReleaseTag do not reference the same manifest"
        }
        $manifestDigest = $currentDigest
        $sourceRef = "${Image}@$currentDigest"
        $presentCount++
    }

    $skip = $presentCount -gt 0
    $missingExact = ""
    if ($skip -and $missing.Count -eq 1) {
        $missingExact = $missing[0]
    }
    return [pscustomobject]@{
        Skip = $skip
        SourceRef = $sourceRef
        MissingExact = $missingExact
        PresentCount = $presentCount
        Digest = $manifestDigest
    }
}

function Compare-StableVersion {
    param(
        [Parameter(Mandatory = $true)][string]$Left,
        [Parameter(Mandatory = $true)][string]$Right
    )

    foreach ($value in @($Left, $Right)) {
        if ($value -notmatch '^\d+\.\d+\.\d+$') {
            throw "stable image version annotation is missing or invalid"
        }
    }
    $leftParts = @($Left.Split('.') | ForEach-Object { [System.Numerics.BigInteger]::Parse($_) })
    $rightParts = @($Right.Split('.') | ForEach-Object { [System.Numerics.BigInteger]::Parse($_) })
    for ($index = 0; $index -lt 3; $index++) {
        $comparison = $leftParts[$index].CompareTo($rightParts[$index])
        if ($comparison -ne 0) {
            return $comparison
        }
    }
    return 0
}

function Get-StableAliasDecision {
    param(
        [Parameter(Mandatory = $true)]$AliasInspection,
        [Parameter(Mandatory = $true)][string]$TargetRaw,
        [Parameter(Mandatory = $true)][string]$TargetVersion,
        [Parameter(Mandatory = $true)][string]$TargetRevision
    )

    if ($AliasInspection.State -eq "missing") {
        return "advance"
    }
    if ((Get-ManifestDigest -Raw $AliasInspection.Raw) -eq (Get-ManifestDigest -Raw $TargetRaw)) {
        return "keep"
    }

    $aliasVersion = Get-ManifestAnnotation -Raw $AliasInspection.Raw -Name "org.opencontainers.image.version"
    $comparison = Compare-StableVersion -Left $aliasVersion -Right $TargetVersion
    if ($comparison -gt 0) {
        return "keep_newer"
    }
    if ($comparison -eq 0) {
        $aliasRevision = Get-ManifestAnnotation -Raw $AliasInspection.Raw -Name "org.opencontainers.image.revision"
        if ($aliasRevision -ne $TargetRevision) {
            throw "stable alias has the target version but a different source revision"
        }
    }
    return "advance"
}

function Assert-MajorMinorAliasVersion {
    param(
        [Parameter(Mandatory = $true)][string]$Raw,
        [Parameter(Mandatory = $true)][string]$MajorMinor
    )

    $aliasVersion = Get-ManifestAnnotation -Raw $Raw -Name "org.opencontainers.image.version"
    [void](Compare-StableVersion -Left $aliasVersion -Right $aliasVersion)
    $parts = $aliasVersion.Split('.')
    if ("$($parts[0]).$($parts[1])" -ne $MajorMinor) {
        throw "major/minor alias points outside its release line"
    }
}

function Invoke-ImageToolsCreate {
    param(
        [Parameter(Mandatory = $true)][string]$Target,
        [Parameter(Mandatory = $true)][string]$Source
    )

    $result = Invoke-CapturedProcess -FilePath "docker" -ArgumentList @(
        "buildx", "imagetools", "create", "--tag", $Target, $Source
    )
    if ($result.ExitCode -ne 0) {
        throw "registry tag update failed for $Target"
    }
}

function Invoke-StableAliasPromotion {
    param(
        [Parameter(Mandatory = $true)][string]$Image,
        [Parameter(Mandatory = $true)][string]$ReleaseTag,
        [Parameter(Mandatory = $true)][string]$Version,
        [Parameter(Mandatory = $true)][string]$MajorMinor,
        [Parameter(Mandatory = $true)][string]$SourceSha,
        [int]$Attempts = 3,
        [int]$DelaySeconds = 2,
        [scriptblock]$InspectRunner = $null,
        [scriptblock]$SleepAction = $null,
        [scriptblock]$CreateRunner = $null
    )

    if ($null -eq $CreateRunner) {
        $CreateRunner = { param($Target, $Source) Invoke-ImageToolsCreate -Target $Target -Source $Source }
    }
    $targetRef = "${Image}:$ReleaseTag"
    $target = Get-RegistryManifest -Reference $targetRef -Attempts $Attempts -DelaySeconds $DelaySeconds -InspectRunner $InspectRunner -SleepAction $SleepAction
    if ($target.State -ne "present") {
        throw "stable alias target is missing"
    }
    if ((Get-ManifestAnnotation -Raw $target.Raw -Name "org.opencontainers.image.revision") -ne $SourceSha -or
        (Get-ManifestAnnotation -Raw $target.Raw -Name "org.opencontainers.image.version") -ne $Version) {
        throw "stable alias target has unexpected source annotations"
    }
    $targetDigest = Get-ManifestDigest -Raw $target.Raw
    $expectedDigests = [ordered]@{}

    foreach ($alias in @($MajorMinor, "latest")) {
        $aliasRef = "${Image}:$alias"
        $inspection = Get-RegistryManifest -Reference $aliasRef -Attempts $Attempts -DelaySeconds $DelaySeconds -InspectRunner $InspectRunner -SleepAction $SleepAction
        if ($alias -eq $MajorMinor -and $inspection.State -eq "present") {
            Assert-MajorMinorAliasVersion -Raw $inspection.Raw -MajorMinor $MajorMinor
        }
        $decision = Get-StableAliasDecision -AliasInspection $inspection -TargetRaw $target.Raw -TargetVersion $Version -TargetRevision $SourceSha
        if ($decision -eq "keep") {
            Write-Host "$aliasRef already references $ReleaseTag"
            $expectedDigests[$alias] = $targetDigest
            continue
        }
        if ($decision -eq "keep_newer") {
            Write-Host "$aliasRef already references a newer stable release; leaving it unchanged"
            $expectedDigests[$alias] = Get-ManifestDigest -Raw $inspection.Raw
            continue
        }

        & $CreateRunner $aliasRef "${Image}@$targetDigest" | Out-Null
        $updated = Get-RegistryManifest -Reference $aliasRef -Attempts $Attempts -DelaySeconds $DelaySeconds -InspectRunner $InspectRunner -SleepAction $SleepAction
        if ($updated.State -ne "present" -or (Get-ManifestDigest -Raw $updated.Raw) -ne $targetDigest) {
            throw "stable alias verification failed for $aliasRef"
        }
        $expectedDigests[$alias] = $targetDigest
    }
    return [pscustomobject]@{
        MajorMinorDigest = [string]$expectedDigests[$MajorMinor]
        LatestDigest = [string]$expectedDigests["latest"]
    }
}

function Invoke-PublicAliasVerification {
    param(
        [Parameter(Mandatory = $true)][string]$Image,
        [Parameter(Mandatory = $true)][string]$MajorMinor,
        [Parameter(Mandatory = $true)][string]$MajorMinorDigest,
        [Parameter(Mandatory = $true)][string]$LatestDigest,
        [int]$Attempts = 3,
        [int]$DelaySeconds = 2,
        [scriptblock]$InspectRunner = $null,
        [scriptblock]$SleepAction = $null
    )

    foreach ($expected in @{
            $MajorMinor = $MajorMinorDigest
            latest = $LatestDigest
        }.GetEnumerator()) {
        if ($expected.Value -notmatch '^sha256:[0-9a-f]{64}$') {
            throw "expected stable alias digest is invalid"
        }
        $reference = "${Image}:$($expected.Key)"
        $inspection = Get-RegistryManifest -Reference $reference -Attempts $Attempts -DelaySeconds $DelaySeconds -InspectRunner $InspectRunner -SleepAction $SleepAction
        if ($inspection.State -ne "present" -or (Get-ManifestDigest -Raw $inspection.Raw) -ne $expected.Value) {
            throw "public stable alias verification failed for $reference"
        }
    }
}

function Write-GitHubOutput {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [AllowEmptyString()][string]$Value
    )

    if ([string]::IsNullOrWhiteSpace($env:GITHUB_OUTPUT)) {
        throw "GITHUB_OUTPUT is not available"
    }
    if ($Value -match "[`r`n]") {
        throw "multi-line GitHub output is not allowed"
    }
    Add-Content -LiteralPath $env:GITHUB_OUTPUT -Value "$Name=$Value" -Encoding utf8
}

function Assert-ReleaseArguments {
    param([string[]]$Names)

    foreach ($name in $Names) {
        $value = Get-Variable -Name $name -ValueOnly
        if ([string]::IsNullOrWhiteSpace([string]$value)) {
            throw "$name is required"
        }
    }
}

if ($MyInvocation.InvocationName -ne '.') {
    switch ($Mode) {
        "preflight" {
            Assert-ReleaseArguments -Names @("Image", "ReleaseTag", "Version", "SourceSha")
            $result = Invoke-ExactTagPreflight -Image $Image -ReleaseTag $ReleaseTag -Version $Version -SourceSha $SourceSha -Attempts $InspectAttempts -DelaySeconds $InspectDelaySeconds
            Write-GitHubOutput -Name "skip" -Value $result.Skip.ToString().ToLowerInvariant()
            Write-GitHubOutput -Name "source_ref" -Value $result.SourceRef
            Write-GitHubOutput -Name "missing_exact" -Value $result.MissingExact
        }
        "verify-exact" {
            Assert-ReleaseArguments -Names @("Image", "ReleaseTag", "Version", "SourceSha")
            $result = Invoke-ExactTagPreflight -Image $Image -ReleaseTag $ReleaseTag -Version $Version -SourceSha $SourceSha -Attempts $InspectAttempts -DelaySeconds $InspectDelaySeconds
            if ($result.PresentCount -ne 2 -or $result.MissingExact -ne "") {
                throw "both immutable exact tags must exist"
            }
        }
        "promote" {
            Assert-ReleaseArguments -Names @("Image", "ReleaseTag", "Version", "SourceSha", "MajorMinor")
            $result = Invoke-StableAliasPromotion -Image $Image -ReleaseTag $ReleaseTag -Version $Version -MajorMinor $MajorMinor -SourceSha $SourceSha -Attempts $InspectAttempts -DelaySeconds $InspectDelaySeconds
            Write-GitHubOutput -Name "major_minor_digest" -Value $result.MajorMinorDigest
            Write-GitHubOutput -Name "latest_digest" -Value $result.LatestDigest
        }
        "verify-aliases" {
            Assert-ReleaseArguments -Names @("Image", "MajorMinor", "ExpectedMajorMinorDigest", "ExpectedLatestDigest")
            Invoke-PublicAliasVerification -Image $Image -MajorMinor $MajorMinor -MajorMinorDigest $ExpectedMajorMinorDigest -LatestDigest $ExpectedLatestDigest -Attempts $InspectAttempts -DelaySeconds $InspectDelaySeconds
        }
        default {
            throw "Mode must be preflight, verify-exact, promote, or verify-aliases"
        }
    }
}
