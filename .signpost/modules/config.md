---
type: Module
title: internal/config
description: 2 go files; 26 exported symbols.
resource: git://github.com/3rg0n/signpost@aa1287096af85ed210771f340e57c53aeebd0082/internal/config
generated: { by: signpost/dev, at: "2026-08-10" }
attributes:
  - { name: commits, value: "1" }
  - { name: exported, value: "26" }
  - { name: files, value: "2" }
  - { name: first_commit, value: "2026-08-02" }
  - { name: last_commit, value: "2026-08-02" }
  - { name: lines_added, value: "820" }
  - { name: lines_removed, value: "0" }
  - { name: package, value: config }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 100% }
edges:
  - { kind: imports, to: ./hook.md, confidence: extracted, weight: 1, source: internal/config/config.go }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 1, source: internal/config/config.go }
  - { kind: imports, to: ./model.md, confidence: extracted, weight: 2, source: internal/config/config.go }
---
# internal/config

<!-- signpost:managed:summary -->
2 go files; 26 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
2 files:
- `internal/config/config.go`
- `internal/config/config_test.go`

- **Exports** (26): `Config`, `File`, `Load`, `TestAbsentFileIsNotAnError`, `TestBadValuesAreRejected`, `TestBooleanSpellings`, `TestConfigIsNotReadFromAParentDirectory`, `TestCredentialKeysAreRejectedByName`, `TestDashedConfigurableKeySuggestsTheUnderscoreSpelling`, `TestDashedSpellingsAreAlsoRefused`, `TestEmptyDocumentIsNotAnError`, `TestErrorsNameTheLine`, `TestEveryConfigurableKeyIsRead`, `TestEveryRefusalGivesAReason`, `TestFalseIsRead`, `TestIgnoreAcceptsASingleScalar`, `TestInterpolationIsRejected`, `TestMalformedFileIsRejected`, `TestNoKeyIsBothSettableAndRefused`, `TestNonConfigurableFlagsCannotBeSet`, `TestNonMappingDocumentIsRejected`, `TestTheFirstBadKeyIsReported`, `TestTheOtherExtensionIsReported`, `TestUnbracedDollarIsNotInterpolation`, `TestUnknownKeyIsRejected`, `TestUnreadableFileIsAnError`

- **Imports**: [internal/hook](./hook.md) ×1, [internal/manifest](./manifest.md) ×1, [internal/model](./model.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
