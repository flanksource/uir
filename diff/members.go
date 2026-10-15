package diff

import (
	"fmt"

	"github.com/flanksource/uir"
)

// members diffs a container's member list: members pair by symbol key, never by
// position or hash, so a member that is not the same symbol on both sides is a
// delete and an add.
func (d differ) members(path string, before, after []uir.Node) DiffResult {
	var result DiffResult
	if !d.walkMembers {
		return result
	}
	pairs := pairByKey(before, after)
	paired := make(map[int]bool, len(pairs))
	for bi, node := range before {
		ai, ok := pairs[bi]
		if !ok {
			result.Add(DiffChange{Kind: ChangeDeleted, Path: path, Before: newDiffNodeRef(node, "")})
			continue
		}
		paired[ai] = true
		result.merge(d.node(node, after[ai]))
	}
	for ai, node := range after {
		if !paired[ai] {
			result.Add(DiffChange{Kind: ChangeAdded, Path: path, After: newDiffNodeRef(node, "")})
		}
	}
	return result
}

// pairByKey maps each before index to the after index of the same symbol key.
// A key repeated within a list pairs its occurrences in order.
func pairByKey(before, after []uir.Node) map[int]int {
	afterByKey := map[string][]int{}
	for i, node := range after {
		key := nodePath(node)
		afterByKey[key] = append(afterByKey[key], i)
	}
	pairs := map[int]int{}
	for i, node := range before {
		key := nodePath(node)
		if queue := afterByKey[key]; len(queue) > 0 {
			pairs[i] = queue[0]
			afterByKey[key] = queue[1:]
		}
	}
	return pairs
}

// reorderedColumns reports the columns present on both sides whose relative
// order changed. The columns of the longest run that kept its relative order
// stay put, so inserting or dropping a column moves nothing and moving one
// column reports only that column.
func reorderedColumns(path string, before, after []uir.RecordColumn) DiffResult {
	var result DiffResult
	beforeOf := map[int]int{}
	for bi, ai := range pairByKey(uir.NodesOf(before), uir.NodesOf(after)) {
		beforeOf[ai] = bi
	}
	var afterPositions, beforePositions []int
	for ai := range after {
		if bi, ok := beforeOf[ai]; ok {
			afterPositions = append(afterPositions, ai)
			beforePositions = append(beforePositions, bi)
		}
	}
	kept := longestIncreasingSubsequence(beforePositions)
	for i, ai := range afterPositions {
		if kept[i] {
			continue
		}
		bi := beforePositions[i]
		result.Add(DiffChange{
			Kind:   ChangeMoved,
			Path:   path,
			Detail: fmt.Sprintf("column position %d -> %d", bi+1, ai+1),
			Before: newDiffNodeRef(before[bi], ""),
			After:  newDiffNodeRef(after[ai], ""),
		})
	}
	return result
}

// longestIncreasingSubsequence marks the elements of one longest strictly
// increasing subsequence of seq; ties keep the earliest-ending one.
func longestIncreasingSubsequence(seq []int) []bool {
	length := make([]int, len(seq))
	prev := make([]int, len(seq))
	best := -1
	for i := range seq {
		length[i], prev[i] = 1, -1
		for j := 0; j < i; j++ {
			if seq[j] < seq[i] && length[j]+1 > length[i] {
				length[i], prev[i] = length[j]+1, j
			}
		}
		if best < 0 || length[i] > length[best] {
			best = i
		}
	}
	kept := make([]bool, len(seq))
	for i := best; i >= 0; i = prev[i] {
		kept[i] = true
	}
	return kept
}
