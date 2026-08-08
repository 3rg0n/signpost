#Requires -Version 7.0
#Requires -Modules Pester

using namespace System.Text
using module ./Widget.psm1

Import-Module "$PSScriptRoot/Logging.psm1"

# The near-miss on the runtime side: it opens with the name of the engine module
# Microsoft.PowerShell.Utility and is a separately versioned gallery module. A rule taking
# the first two segments for the runtime hides a dependency nobody is told to patch.
Import-Module Microsoft.PowerShell.Crescendo

Set-StrictMode -Version Latest

function Get-Artifact {
    <#
    .SYNOPSIS
    Fetches a build artifact by name.
    .PARAMETER Name
    The artifact's name.
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][string]$Name
    )
    Write-CorpusLog "fetching $Name"
    [Widget]::Create($Name)
}

# Publishes an artifact to the registry.
function Publish-Artifact {
    param([string]$Path)
    Write-CorpusLog "publishing $Path"
}

function Get-InternalState {
    param()
    @{ strict = $true }
}

Export-ModuleMember -Function Get-Artifact, Publish-Artifact
