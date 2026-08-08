# The near-miss on the dependency side: it opens with every character of the declared
# `Pester` and is not it. A candidate list matched by prefix swallows it and reports a
# dependency this code does not use.
Import-Module PesterExtras

function Write-CorpusLog {
    param([string]$Message)

    $envelope = @"
{"level":"info","message":"$Message"}
"@

    Write-Information $envelope -InformationAction Continue
}
