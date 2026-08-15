#Requires -Version 7.0

# Reaching the corpus schema from PowerShell. Corpus fixture: not imported, not run.
#
# The three shapes go/store/store.go's doc comment describes, in PowerShell's spellings.
#
# PowerShell's here-string is the odd one in this corpus: `@"` ... `"@` is terminated by
# punctuation on a line of its own rather than by an identifier the opener named, so there is
# nothing in the closing line to match against the opening one. And its interpolating and
# literal forms differ by the same quote pair as PHP's heredoc and nowdoc — `@"` expands
# `$table` and `@'` does not — so the delimiter says which of the two shapes a body holds.

Set-StrictMode -Version Latest

$Script:ListOrders = @'
SELECT o.id, o.total
FROM orders o
JOIN customers c ON c.id = o.customer_id
WHERE o.total > @total
'@

function Get-Order {
    <#
    .SYNOPSIS
    Reads two tables.
    #>
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Connection)

    Invoke-SqlQuery -Connection $Connection -Query $Script:ListOrders
}

function Add-Customer {
    <#
    .SYNOPSIS
    Writes the table it names.
    #>
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Connection)

    Invoke-SqlQuery -Connection $Connection -Query 'INSERT INTO customers (id) VALUES (@id)'
}

function Clear-Table {
    <#
    .SYNOPSIS
    The gap: an interpolating here-string whose table is the caller's.
    #>
    [CmdletBinding()]
    param([Parameter(Mandatory)]$Connection, [Parameter(Mandatory)][string]$Table)

    $query = @"
DELETE FROM $Table
WHERE total = 0
"@
    Invoke-SqlQuery -Connection $Connection -Query $query
}

function Write-PurgeWarning {
    <#
    .SYNOPSIS
    Prose.
    #>
    [CmdletBinding()]
    param([Parameter(Mandatory)][string]$Table)

    Write-Warning "could not update the $Table row"
}

Export-ModuleMember -Function Get-Order, Add-Customer, Clear-Table, Write-PurgeWarning
