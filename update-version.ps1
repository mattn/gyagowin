# Get version from main.go
$version = Select-String -Path main.go -Pattern 'const version = "([^"]+)"' | ForEach-Object { $_.Matches.Groups[1].Value }

if (-not $version) {
    Write-Error "Could not find version in main.go"
    exit 1
}

# Parse version (e.g., "0.0.1" -> "0,0,1,0")
$parts = $version -split '\.'
while ($parts.Count -lt 4) {
    $parts += "0"
}
$rcVersion = $parts -join ','
$rcVersionStr = ($parts[0..3] -join '.') + ".0"

Write-Host "Updating gyagowin.rc to version $version ($rcVersion)"

# Update gyagowin.rc (using CP932 encoding for Japanese comments)
$cp932 = [System.Text.Encoding]::GetEncoding(932)
$content = [System.IO.File]::ReadAllText("$PWD\gyagowin.rc", $cp932)
$content = $content -replace '#define VER_FILEVERSION\s+[\d,]+', "#define VER_FILEVERSION             $rcVersion"
$content = $content -replace '#define VER_FILEVERSION_STR\s+"[^"]+"', "#define VER_FILEVERSION_STR         `"$rcVersionStr`""
$content = $content -replace '#define VER_PRODUCTVERSION\s+[\d,]+', "#define VER_PRODUCTVERSION          $rcVersion"
$content = $content -replace '#define VER_PRODUCTVERSION_STR\s+"[^"]+"', "#define VER_PRODUCTVERSION_STR      `"$version`""

[System.IO.File]::WriteAllText("$PWD\gyagowin.rc", $content, $cp932)

Write-Host "Version updated successfully"
