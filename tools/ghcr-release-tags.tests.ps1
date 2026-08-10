$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

. "$PSScriptRoot/ghcr-release-tags.ps1"

function New-InspectResult {
    param(
        [int]$ExitCode,
        [string]$StdOut = "",
        [string]$StdErr = ""
    )
    return [pscustomobject]@{ ExitCode = $ExitCode; StdOut = $StdOut; StdErr = $StdErr }
}

function New-SequenceRunner {
    param([object[]]$Results)

    $queue = [System.Collections.Queue]::new()
    foreach ($result in $Results) {
        $queue.Enqueue($result)
    }
    $state = [pscustomobject]@{ Queue = $queue; Calls = 0 }
    $runner = {
        param($Reference)
        $state.Calls++
        if ($state.Queue.Count -eq 0) {
            throw "no fake inspection left for $Reference"
        }
        return $state.Queue.Dequeue()
    }.GetNewClosure()
    return [pscustomobject]@{ State = $state; Runner = $runner }
}

function New-Manifest {
    param(
        [string]$Revision,
        [string]$Version,
        [string]$Marker
    )
    $sha256 = [System.Security.Cryptography.SHA256]::Create()
    try {
        $digestHex = ([System.BitConverter]::ToString($sha256.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($Marker)))).Replace("-", "").ToLowerInvariant()
    }
    finally {
        $sha256.Dispose()
    }
    return @{
        schemaVersion = 2
        annotations = @{
            "org.opencontainers.image.revision" = $Revision
            "org.opencontainers.image.version" = $Version
        }
        digest = "sha256:$digestHex"
        manifests = @(@{ digest = "sha256:$digestHex" })
    } | ConvertTo-Json -Compress -Depth 5
}

function Assert-Equal {
    param($Actual, $Expected, [string]$Message)
    if ($Actual -ne $Expected) {
        throw "$Message`: got '$Actual', want '$Expected'"
    }
}

function Assert-Throws {
    param([scriptblock]$Action, [string]$Message)
    try {
        & $Action
    }
    catch {
        return
    }
    throw "$Message`: expected failure"
}

$noSleep = { param($Seconds) }
$missing = New-InspectResult -ExitCode 1 -StdErr "ERROR: manifest unknown"
$sequence = New-SequenceRunner -Results @($missing, $missing, $missing)
$inspection = Get-RegistryManifest -Reference "example.invalid/app:missing" -Attempts 3 -DelaySeconds 0 -InspectRunner $sequence.Runner -SleepAction $noSleep
Assert-Equal $inspection.State "missing" "confirmed manifest absence"
Assert-Equal $sequence.State.Calls 3 "missing manifest retry count"

$presentRaw = New-Manifest -Revision "source-a" -Version "1.2.3" -Marker "present"
$sequence = New-SequenceRunner -Results @(
    (New-InspectResult -ExitCode 1 -StdErr "request timed out"),
    (New-InspectResult -ExitCode 0 -StdOut $presentRaw)
)
$inspection = Get-RegistryManifest -Reference "example.invalid/app:present" -Attempts 3 -DelaySeconds 0 -InspectRunner $sequence.Runner -SleepAction $noSleep
Assert-Equal $inspection.State "present" "transient inspect recovery"
Assert-Equal $sequence.State.Calls 2 "transient inspect retry count"

$throttled = New-InspectResult -ExitCode 1 -StdErr "429 Too Many Requests"
$sequence = New-SequenceRunner -Results @($throttled, $throttled, $throttled)
Assert-Throws { Get-RegistryManifest -Reference "example.invalid/app:rate-limited" -Attempts 3 -DelaySeconds 0 -InspectRunner $sequence.Runner -SleepAction $noSleep } "registry throttling must fail closed"

$ambiguousAuth = New-InspectResult -ExitCode 1 -StdErr "repository does not exist or may require authorization: access denied"
$sequence = New-SequenceRunner -Results @($ambiguousAuth, $ambiguousAuth, $ambiguousAuth)
Assert-Throws { Get-RegistryManifest -Reference "example.invalid/app:private" -Attempts 3 -DelaySeconds 0 -InspectRunner $sequence.Runner -SleepAction $noSleep } "ambiguous authorization failure must fail closed"

$timeout = New-InspectResult -ExitCode 1 -StdErr "connection timeout"
$sequence = New-SequenceRunner -Results @($timeout, $missing, $missing)
Assert-Throws { Get-RegistryManifest -Reference "example.invalid/app:mixed" -Attempts 3 -DelaySeconds 0 -InspectRunner $sequence.Runner -SleepAction $noSleep } "mixed transient and missing responses must fail closed"

$sequence = New-SequenceRunner -Results @(
    (New-InspectResult -ExitCode 0 -StdOut $presentRaw),
    $missing, $missing, $missing
)
$preflight = Invoke-ExactTagPreflight -Image "example.invalid/app" -ReleaseTag "v1.2.3" -Version "1.2.3" -SourceSha "source-a" -Attempts 3 -DelaySeconds 0 -InspectRunner $sequence.Runner -SleepAction $noSleep
Assert-Equal $preflight.Skip $true "single exact tag preflight"
Assert-Equal $preflight.SourceRef "example.invalid/app@$(Get-ManifestDigest -Raw $presentRaw)" "repair source reference"
Assert-Equal $preflight.MissingExact "1.2.3" "missing exact tag"

$wrongRevision = New-Manifest -Revision "source-b" -Version "1.2.3" -Marker "wrong"
$sequence = New-SequenceRunner -Results @((New-InspectResult -ExitCode 0 -StdOut $wrongRevision))
Assert-Throws { Invoke-ExactTagPreflight -Image "example.invalid/app" -ReleaseTag "v1.2.3" -Version "1.2.3" -SourceSha "source-a" -Attempts 1 -DelaySeconds 0 -InspectRunner $sequence.Runner -SleepAction $noSleep } "exact tag revision mismatch"

$missingInspection = [pscustomobject]@{ State = "missing"; Raw = "" }
Assert-Equal (Get-StableAliasDecision -AliasInspection $missingInspection -TargetRaw $presentRaw -TargetVersion "1.2.3" -TargetRevision "source-a") "advance" "missing alias decision"
$sameInspection = [pscustomobject]@{ State = "present"; Raw = $presentRaw }
Assert-Equal (Get-StableAliasDecision -AliasInspection $sameInspection -TargetRaw $presentRaw -TargetVersion "1.2.3" -TargetRevision "source-a") "keep" "matching alias decision"
$olderInspection = [pscustomobject]@{ State = "present"; Raw = (New-Manifest -Revision "source-old" -Version "1.2.2" -Marker "older") }
Assert-Equal (Get-StableAliasDecision -AliasInspection $olderInspection -TargetRaw $presentRaw -TargetVersion "1.2.3" -TargetRevision "source-a") "advance" "older alias decision"
$newerInspection = [pscustomobject]@{ State = "present"; Raw = (New-Manifest -Revision "source-new" -Version "1.2.4" -Marker "newer") }
Assert-Equal (Get-StableAliasDecision -AliasInspection $newerInspection -TargetRaw $presentRaw -TargetVersion "1.2.3" -TargetRevision "source-a") "keep_newer" "newer alias rollback prevention"
$newerSameRevisionInspection = [pscustomobject]@{ State = "present"; Raw = (New-Manifest -Revision "source-a" -Version "1.2.4" -Marker "newer-same-revision") }
Assert-Equal (Get-StableAliasDecision -AliasInspection $newerSameRevisionInspection -TargetRaw $presentRaw -TargetVersion "1.2.3" -TargetRevision "source-a") "keep_newer" "newer alias with matching revision rollback prevention"
$conflictInspection = [pscustomobject]@{ State = "present"; Raw = (New-Manifest -Revision "source-conflict" -Version "1.2.3" -Marker "conflict") }
Assert-Throws { Get-StableAliasDecision -AliasInspection $conflictInspection -TargetRaw $presentRaw -TargetVersion "1.2.3" -TargetRevision "source-a" } "same-version alias conflict"
Assert-Throws { Assert-MajorMinorAliasVersion -Raw (New-Manifest -Revision "source-other" -Version "2.0.0" -Marker "other-line") -MajorMinor "1.2" } "major/minor alias release-line mismatch"

$sequence = New-SequenceRunner -Results @(
    (New-InspectResult -ExitCode 0 -StdOut $presentRaw),
    $missing, $missing, $missing,
    (New-InspectResult -ExitCode 0 -StdOut $presentRaw),
    (New-InspectResult -ExitCode 0 -StdOut $newerInspection.Raw)
)
$createState = [pscustomobject]@{ Calls = 0; Target = ""; Source = "" }
$createRunner = {
    param($Target, $Source)
    $createState.Calls++
    $createState.Target = $Target
    $createState.Source = $Source
}.GetNewClosure()
$promotion = Invoke-StableAliasPromotion -Image "example.invalid/app" -ReleaseTag "v1.2.3" -Version "1.2.3" -MajorMinor "1.2" -SourceSha "source-a" -Attempts 3 -DelaySeconds 0 -InspectRunner $sequence.Runner -SleepAction $noSleep -CreateRunner $createRunner
Assert-Equal $createState.Calls 1 "stable alias create count"
Assert-Equal $createState.Target "example.invalid/app:1.2" "stable alias create target"
Assert-Equal $createState.Source "example.invalid/app@$(Get-ManifestDigest -Raw $presentRaw)" "stable alias digest pin"
Assert-Equal $promotion.MajorMinorDigest (Get-ManifestDigest -Raw $presentRaw) "promoted major/minor digest"
Assert-Equal $promotion.LatestDigest (Get-ManifestDigest -Raw $newerInspection.Raw) "newer latest no-op digest"

$workflow = Get-Content -Raw -LiteralPath "$PSScriptRoot/../.github/workflows/release.yml"
$metadataBlock = [regex]::Match($workflow, '(?s)- name: Generate image metadata.*?- name: Build and publish multi-architecture image').Value
if ($metadataBlock -match 'major_minor|value=latest') {
    throw "container build metadata must contain immutable exact tags only"
}
if ($workflow -notmatch '(?m)^\s+queue:\s+max\s*$') {
    throw "release publication must use the non-canceling global queue"
}
if ($workflow -notmatch 'refs/tags/\$tag' -or $workflow -notmatch '\$\{tagRef\}\^\{commit\}') {
    throw "manual release repair must resolve an exact tag ref"
}
if ($workflow -notmatch '(?m)^\s+file:\s+automation/Dockerfile\s*$') {
    throw "container repair must use the trusted automation Dockerfile"
}

function Get-WorkflowJobBlock {
    param([Parameter(Mandatory = $true)][string]$Name)

    $escapedName = [regex]::Escape($Name)
    $match = [regex]::Match($workflow, "(?ms)^  ${escapedName}:\r?\n(?<body>.*?)(?=^  [A-Za-z0-9_-]+:\r?\n|\z)")
    if (-not $match.Success) {
        throw "workflow job $Name was not found"
    }
    return $match.Groups["body"].Value
}

$tagRefPattern = '(?m)^\s+ref:\s+\$\{\{ env\.RELEASE_TAG \}\}\s*$'
$pinnedRefPattern = '(?m)^\s+ref:\s+\$\{\{ needs\.verify\.outputs\.source_sha \}\}\s*$'
$pinnedAssertionPattern = '(?m)^\s+- name: Verify pinned source commit\s*$'

$verifyJob = Get-WorkflowJobBlock -Name "verify"
if ([regex]::Matches($verifyJob, $tagRefPattern).Count -ne 1 -or $verifyJob -match $pinnedRefPattern) {
    throw "verify must resolve the release tag exactly once without self-referencing its source output"
}
if ($verifyJob -notmatch 'refs/tags/\$tag' -or $verifyJob -notmatch '\$\{tagRef\}\^\{commit\}') {
    throw "verify must validate the exact tag ref and its peeled commit"
}

foreach ($jobName in @("build", "container")) {
    $job = Get-WorkflowJobBlock -Name $jobName
    if ([regex]::Matches($job, $pinnedRefPattern).Count -ne 1 -or $job -match $tagRefPattern) {
        throw "$jobName must check out only the verified source commit"
    }
    if ([regex]::Matches($job, $pinnedAssertionPattern).Count -ne 1) {
        throw "$jobName must assert the pinned source commit"
    }
}

Write-Host "GHCR release tag policy tests passed"
