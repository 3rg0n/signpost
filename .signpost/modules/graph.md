---
type: Module
title: internal/graph
description: 4 go files; 49 exported symbols.
resource: git://github.com/3rg0n/signpost@82cb942ea2098d8c7d0a14ddd42b71a21b44db8b/internal/graph
generated: { by: signpost/dev, at: "2026-08-11" }
attributes:
  - { name: commits, value: "3" }
  - { name: exported, value: "49" }
  - { name: files, value: "4" }
  - { name: first_commit, value: "2026-07-29" }
  - { name: last_commit, value: "2026-08-10" }
  - { name: lines_added, value: "1382" }
  - { name: lines_removed, value: "4" }
  - { name: package, value: graph }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 67% }
edges:
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 3 }
---
# internal/graph

<!-- signpost:managed:summary -->
4 go files; 49 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
4 files:
- `internal/graph/graph.go`
- `internal/graph/graph_test.go`
- `internal/graph/louvain.go`
- `internal/graph/metrics.go`

- **Exports** (49): `Ambiguous`, `Bridge`, `Confidence`, `Degree`, `Edge`, `EdgeCalls`, `EdgeCoChanges`, `EdgeConfigures`, `EdgeDefines`, `EdgeDeploys`, `EdgeDocuments`, `EdgeImplements`, `EdgeImports`, `EdgeKind`, `EdgeOwns`, `EdgeTestedBy`, `Extracted`, `Graph`, `Graph.AddEdge`, `Graph.AddNode`, `Graph.Bridges`, `Graph.Clusters`, `Graph.Components`, `Graph.Counts`, `Graph.Cycles`, `Graph.Dangling`, `Graph.Degrees`, `Graph.DropDangling`, `Graph.Edges`, `Graph.EdgesFrom`, `Graph.EdgesTo`, `Graph.Has`, `Graph.Hubs`, `Graph.Node`, `Graph.Nodes`, `Graph.NodesOfKind`, `Graph.Orphans`, `Graph.Path`, `Inferred`, `Kind`, `KindDataStore`, `KindDocument`, `KindExternal`, `KindInterface`, `KindModule`, `KindService`, `KindSymbol`, `New`, `Node`

- **Changes with**: [\(repository root\)](./root.md) ×3
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
