---
type: Module
title: internal/graph
description: 4 go files; 51 exported symbols.
resource: git://github.com/3rg0n/signpost@f1c85cb4603e356013b50452bd0aa50f8b4d1192/internal/graph
generated: { by: signpost/dev, at: "2026-08-15" }
attributes:
  - { name: commits, value: "4" }
  - { name: exported, value: "51" }
  - { name: files, value: "4" }
  - { name: first_commit, value: "2026-07-29" }
  - { name: last_commit, value: "2026-08-15" }
  - { name: lines_added, value: "1394" }
  - { name: lines_removed, value: "4" }
  - { name: package, value: graph }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 75% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./export.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./okf.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 4 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 2 }
---
# internal/graph

<!-- signpost:managed:summary -->
4 go files; 51 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
4 files:
- `internal/graph/graph.go`
- `internal/graph/graph_test.go`
- `internal/graph/louvain.go`
- `internal/graph/metrics.go`

- **Exports** (51): `Ambiguous`, `Bridge`, `Confidence`, `Degree`, `Edge`, `EdgeCalls`, `EdgeCoChanges`, `EdgeConfigures`, `EdgeDefines`, `EdgeDeploys`, `EdgeDocuments`, `EdgeImplements`, `EdgeImports`, `EdgeKind`, `EdgeOwns`, `EdgePrecedes`, `EdgeTestedBy`, `Extracted`, `Graph`, `Graph.AddEdge`, `Graph.AddNode`, `Graph.Bridges`, `Graph.Clusters`, `Graph.Components`, `Graph.Counts`, `Graph.Cycles`, `Graph.Dangling`, `Graph.Degrees`, `Graph.DropDangling`, `Graph.Edges`, `Graph.EdgesFrom`, `Graph.EdgesTo`, `Graph.Has`, `Graph.Hubs`, `Graph.Node`, `Graph.Nodes`, `Graph.NodesOfKind`, `Graph.Orphans`, `Graph.Path`, `Inferred`, `Kind`, `KindDataStore`, `KindDocument`, `KindExternal`, `KindInterface`, `KindModule`, `KindPipeline`, `KindService`, `KindSymbol`, `New`, `Node`

- **Changes with**: [internal/assemble](./assemble.md) ×2, [internal/export](./export.md) ×2, [internal/okf](./okf.md) ×2, [\(repository root\)](./root.md) ×4, [cmd/signpost](./signpost.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
