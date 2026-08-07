package graph

import "sort"

// Louvain community detection, hand-written and fully deterministic.
//
// Why this and not a library: the alternatives in this space (Leiden via
// graspologic, or NetworkX's implementations) pull a numeric stack — and in
// graspologic's case a JIT compiler — whose CVEs we would inherit without a
// direct remediation path. Louvain is ~150 lines of arithmetic. It is the
// clearly cheaper option once "we must be able to patch every dependency
// ourselves" is a hard constraint.
//
// Why not label propagation, which is a third the size: it collapses a graph of
// two dense groups joined by a single edge into one community. Synchronous
// propagation with a lowest-label tie-break degenerates into a min-label flood
// across any connected graph. That is the documented giant-community failure
// mode, and it defeats everything the partition is for: a cluster count of one
// for every repository, an empty Bridges — no edge can cross a boundary that
// does not exist — and every diagram drawn as a single box.
//
// Determinism, which CI depends on because the bundle is committed:
//   - nodes are processed in sorted-ID order (caller supplies the order)
//   - candidate communities are evaluated in ascending community index
//   - gain ties break toward the lowest community index (strict >)
//   - aggregation preserves index order, so pass N+1 inherits pass N's order
//
// There is no seed and no randomised restart, so the same graph yields the same
// partition on every run and every platform.

// louvain returns node ID -> community label. ids must be sorted; adj must be
// symmetric with sorted, deduped neighbour lists. Edges are unweighted (each
// counts 1), which matches how the graph is built: edge multiplicity is carried
// as Edge.Weight for reporting, but coupling strength for clustering purposes
// is better served by treating a relationship as present or absent.
func louvain(ids []string, adj map[string][]string) map[string]int {
	n := len(ids)
	out := make(map[string]int, n)
	if n == 0 {
		return out
	}

	// Index nodes by sorted position so all internal work is on ints.
	idx := make(map[string]int, n)
	for i, id := range ids {
		idx[id] = i
	}

	// Level 0 graph: adjacency as index pairs. selfLoops tracks weight folded
	// into a node by aggregation (zero at level 0).
	type lvlGraph struct {
		size      int
		neighbors [][]int     // neighbor index lists, ascending
		weights   [][]float64 // parallel to neighbors
		selfLoops []float64   // internal weight collapsed into each node
	}

	cur := lvlGraph{
		size:      n,
		neighbors: make([][]int, n),
		weights:   make([][]float64, n),
		selfLoops: make([]float64, n),
	}
	for i, id := range ids {
		nbrs := adj[id]
		cur.neighbors[i] = make([]int, 0, len(nbrs))
		cur.weights[i] = make([]float64, 0, len(nbrs))
		for _, nb := range nbrs {
			j, ok := idx[nb]
			if !ok || j == i {
				continue
			}
			cur.neighbors[i] = append(cur.neighbors[i], j)
			cur.weights[i] = append(cur.weights[i], 1)
		}
	}

	// membership maps original node index -> current level's node index.
	membership := make([]int, n)
	for i := range membership {
		membership[i] = i
	}

	const maxLevels = 20
	for level := 0; level < maxLevels; level++ {
		comm, improved := louvainOnePass(cur.size, cur.neighbors, cur.weights, cur.selfLoops)
		if !improved {
			break
		}

		// Renumber communities to a dense range, preserving ascending order of
		// their lowest member index so the next level's ordering is stable.
		remap := make(map[int]int)
		var order []int
		for v := 0; v < cur.size; v++ {
			if _, ok := remap[comm[v]]; !ok {
				remap[comm[v]] = len(order)
				order = append(order, comm[v])
			}
		}
		newSize := len(order)
		if newSize == cur.size {
			// Nothing merged; further levels cannot improve.
			break
		}
		for v := 0; v < cur.size; v++ {
			comm[v] = remap[comm[v]]
		}

		// Push the mapping down to original nodes.
		for i := 0; i < n; i++ {
			membership[i] = comm[membership[i]]
		}

		// Aggregate: one node per community, edges summed, intra-community
		// weight folded into self-loops.
		agg := make([]map[int]float64, newSize)
		for i := range agg {
			agg[i] = make(map[int]float64)
		}
		self := make([]float64, newSize)
		for v := 0; v < cur.size; v++ {
			cv := comm[v]
			self[cv] += cur.selfLoops[v]
			for k, w := range cur.neighbors[v] {
				cw := comm[w]
				weight := cur.weights[v][k]
				if cv == cw {
					// Each intra edge is seen from both endpoints; halve so the
					// self-loop holds the edge weight once (doubled by
					// convention in the modularity formula below).
					self[cv] += weight / 2
					continue
				}
				agg[cv][cw] += weight
			}
		}

		next := lvlGraph{
			size:      newSize,
			neighbors: make([][]int, newSize),
			weights:   make([][]float64, newSize),
			selfLoops: self,
		}
		for i := 0; i < newSize; i++ {
			keys := make([]int, 0, len(agg[i]))
			for k := range agg[i] {
				keys = append(keys, k)
			}
			sort.Ints(keys) // ascending neighbour order => deterministic passes
			next.neighbors[i] = keys
			next.weights[i] = make([]float64, len(keys))
			for k, j := range keys {
				next.weights[i][k] = agg[i][j]
			}
		}
		cur = next
	}

	for i, id := range ids {
		out[id] = membership[i]
	}
	return out
}

// louvainOnePass runs the local-moving phase: repeatedly move each node to the
// neighbouring community giving the largest modularity gain, until no move
// improves. Returns the community of each node and whether anything moved.
//
// Modularity gain for moving isolated node v into community c is proportional
// to  k_{v,in} - (sum_tot(c) * k_v) / (2m), where k_{v,in} is the edge weight
// from v into c, sum_tot(c) is the total degree of c, and k_v is v's degree.
// Constant factors are dropped since only the argmax matters.
func louvainOnePass(size int, neighbors [][]int, weights [][]float64, selfLoops []float64) ([]int, bool) {
	comm := make([]int, size)
	degree := make([]float64, size)  // k_v, including self-loops (counted twice)
	commTot := make([]float64, size) // sum_tot per community

	var twoM float64
	for v := 0; v < size; v++ {
		comm[v] = v
		d := 2 * selfLoops[v]
		for _, w := range weights[v] {
			d += w
		}
		degree[v] = d
		commTot[v] = d
		twoM += d
	}
	if twoM == 0 {
		return comm, false
	}

	anyMoved := false
	const maxRounds = 100
	for round := 0; round < maxRounds; round++ {
		moved := false
		for v := 0; v < size; v++ { // ascending: caller sorted the ids
			// Weight from v into each neighbouring community.
			links := make(map[int]float64, len(neighbors[v]))
			for k, w := range neighbors[v] {
				links[comm[w]] += weights[v][k]
			}

			old := comm[v]
			// Remove v from its community before evaluating gains.
			commTot[old] -= degree[v]

			bestComm := old
			bestGain := links[old] - commTot[old]*degree[v]/twoM

			cands := make([]int, 0, len(links))
			for c := range links {
				cands = append(cands, c)
			}
			sort.Ints(cands) // ascending => lowest index wins a tie
			for _, c := range cands {
				if c == old {
					continue
				}
				gain := links[c] - commTot[c]*degree[v]/twoM
				if gain > bestGain {
					bestGain, bestComm = gain, c
				}
			}

			commTot[bestComm] += degree[v]
			comm[v] = bestComm
			if bestComm != old {
				moved = true
				anyMoved = true
			}
		}
		if !moved {
			break
		}
	}
	return comm, anyMoved
}
