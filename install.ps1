param(
    [string]$Version = 'latest',
    [string]$InstallDir = "$env:LOCALAPPDATA\Programs\acn"
)

$ErrorActionPreference = 'Stop'

$repo = 'Windyskr/agent-completion-notification'
$apiBase = "https://api.github.com/repos/$repo"

if ([string]::IsNullOrWhiteSpace($InstallDir)) {
    throw 'InstallDir 不能为空。'
}

$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
switch ($architecture) {
    'x64' { $assetArchitecture = 'amd64' }
    'arm64' { $assetArchitecture = 'arm64' }
    default { throw "暂不支持 Windows $architecture。" }
}

$headers = @{
    Accept = 'application/vnd.github+json'
    'User-Agent' = 'acn-installer'
}

if ($Version -eq 'latest') {
    $release = Invoke-RestMethod -Uri "$apiBase/releases/latest" -Headers $headers
}
else {
    $tag = if ($Version.StartsWith('v')) { $Version } else { "v$Version" }
    $release = Invoke-RestMethod -Uri "$apiBase/releases/tags/$tag" -Headers $headers
}

$releaseVersion = $release.tag_name.TrimStart('v')
$archiveName = "acn_${releaseVersion}_windows_${assetArchitecture}.zip"
$archiveAsset = $release.assets | Where-Object { $_.name -eq $archiveName } | Select-Object -First 1
$checksumAsset = $release.assets | Where-Object { $_.name -eq 'checksums.txt' } | Select-Object -First 1
if (-not $archiveAsset -or -not $checksumAsset) {
    throw "Release $($release.tag_name) 缺少 $archiveName 或 checksums.txt。"
}

$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("acn-install-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tempDir | Out-Null

try {
    $archivePath = Join-Path $tempDir $archiveName
    $checksumPath = Join-Path $tempDir 'checksums.txt'
    Invoke-WebRequest -Uri $archiveAsset.browser_download_url -Headers $headers -OutFile $archivePath
    Invoke-WebRequest -Uri $checksumAsset.browser_download_url -Headers $headers -OutFile $checksumPath

    $checksumLine = Get-Content -LiteralPath $checksumPath -Encoding UTF8 |
        Where-Object { $_ -match "\s\*?$([regex]::Escape($archiveName))$" } |
        Select-Object -First 1
    if (-not $checksumLine) {
        throw "checksums.txt 中找不到 $archiveName。"
    }

    $expectedHash = ($checksumLine -split '\s+')[0].ToLowerInvariant()
    $actualHash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -ne $expectedHash) {
        throw "SHA-256 校验失败：期望 $expectedHash，实际 $actualHash。"
    }

    $extractDir = Join-Path $tempDir 'extracted'
    Expand-Archive -LiteralPath $archivePath -DestinationPath $extractDir
    $sourceExe = Get-ChildItem -LiteralPath $extractDir -Filter 'acn.exe' -Recurse |
        Select-Object -First 1
    if (-not $sourceExe) {
        throw '压缩包中找不到 acn.exe。'
    }

    $resolvedInstallDir = [System.IO.Path]::GetFullPath($InstallDir)
    New-Item -ItemType Directory -Path $resolvedInstallDir -Force | Out-Null
    Copy-Item -LiteralPath $sourceExe.FullName -Destination (Join-Path $resolvedInstallDir 'acn.exe') -Force

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $pathEntries = @($userPath -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    $alreadyOnPath = $pathEntries | Where-Object {
        [string]::Equals($_.TrimEnd('\'), $resolvedInstallDir.TrimEnd('\'), [StringComparison]::OrdinalIgnoreCase)
    }
    if (-not $alreadyOnPath) {
        $newUserPath = (@($pathEntries) + $resolvedInstallDir) -join ';'
        [Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')
    }
    if (-not (($env:Path -split ';') | Where-Object {
        [string]::Equals($_.TrimEnd('\'), $resolvedInstallDir.TrimEnd('\'), [StringComparison]::OrdinalIgnoreCase)
    })) {
        $env:Path = "$resolvedInstallDir;$env:Path"
    }

    & (Join-Path $resolvedInstallDir 'acn.exe') version
    Write-Host "已安装到 $resolvedInstallDir"
    Write-Host '请在当前终端运行 acn install；其他已打开的终端需重启后才能读取新的 PATH。'
}
finally {
    if (Test-Path -LiteralPath $tempDir) {
        Remove-Item -LiteralPath $tempDir -Recurse -Force
    }
}
