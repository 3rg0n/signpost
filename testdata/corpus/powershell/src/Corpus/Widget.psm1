using namespace System.Collections.Generic

# A PowerShell class is shared only by `using module` — an Import-Module does not bring one
# across — so this file is where the repository's real internal edges are written.
class Widget {
    [string]$Name
    [int]$Count = 0
    $Metadata
    hidden [hashtable]$cache = @{}

    Widget([string]$name) {
        $this.Name = $name
    }

    [string] Describe() {
        return "$($this.Name) x $($this.Count)"
    }

    static [Widget] Create([string]$name) {
        return [Widget]::new($name)
    }

    hidden [void] Reset() {
        $this.cache.Clear()
    }
}

enum Severity {
    Low
    High
}

function New-Widget {
    param([string]$Name)
    [Widget]::Create($Name)
}

function _assert-Widget {
    param($w)
    if (-not $w) { throw "null" }
}
