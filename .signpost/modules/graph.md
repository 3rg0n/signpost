---
type: Module
title: internal/graph
description: 4 go files; 69 exported symbols.
resource: git://github.com/3rg0n/signpost@aa1287096af85ed210771f340e57c53aeebd0082/internal/graph
generated: { by: signpost/dev, at: "2026-08-10" }
attributes:
  - { name: commits, value: "3" }
  - { name: exported, value: "69" }
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
4 go files; 69 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
4 files:
- `internal/graph/graph.go`
- `internal/graph/graph_test.go`
- `internal/graph/louvain.go`
- `internal/graph/metrics.go`

- **Exports** (69): `Ambiguous`, `Bridge`, `Confidence`, `Degree`, `Edge`, `EdgeCalls`, `EdgeCoChanges`, `EdgeConfigures`, `EdgeDefines`, `EdgeDeploys`, `EdgeDocuments`, `EdgeImplements`, `EdgeImports`, `EdgeKind`, `EdgeOwns`, `EdgeTestedBy`, `Extracted`, `Graph`, `Graph.AddEdge`, `Graph.AddNode`, `Graph.Bridges`, `Graph.Clusters`, `Graph.Components`, `Graph.Counts`, `Graph.Cycles`, `Graph.Dangling`, `Graph.Degrees`, `Graph.DropDangling`, `Graph.Edges`, `Graph.EdgesFrom`, `Graph.EdgesTo`, `Graph.Has`, `Graph.Hubs`, `Graph.Node`, `Graph.Nodes`, `Graph.NodesOfKind`, `Graph.Orphans`, `Graph.Path`, `Inferred`, `Kind`, `KindDataStore`, `KindDocument`, `KindExternal`, `KindInterface`, `KindModule`, `KindService`, `KindSymbol`, `New`, `Node`, `TestAddEdgeDropsSelfEdges`, `TestAddEdgeKeepsStrongerConfidenceRegardlessOfOrder`, `TestAddEdgeMergesWeightAndKeepsStrongerConfidence`, `TestAddNodeKindConflictIsAnError`, `TestAddNodeMergesWithoutClobbering`, `TestAddNodeRejectsEmptyID`, `TestBridgesCrossClustersOnly`, `TestClustersAreDeterministic`, `TestClustersSeparateDenseGroups`, `TestComponentsSeparatesIslands`, `TestCyclesDeepChainDoesNotOverflow`, and 9 more

- **Changes with**: [\(repository root\)](./root.md) ×3
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
