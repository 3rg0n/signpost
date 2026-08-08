#!/usr/bin/env pwsh
# A script rather than a module: the top-level param block is what makes it one.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Configuration
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# A Windows separator, which is how real PowerShell is written even where `/` would work.
Import-Module "$PSScriptRoot\..\src\Corpus\Corpus.psm1"

function Invoke-Compile {
    param([string]$Config)

    Invoke-RestMethod -Uri $env:CORPUS_HOOK -Body @"
{"config":"$Config"}
"@ -ContentType 'application/json'
}

Invoke-Compile -Config $Configuration
