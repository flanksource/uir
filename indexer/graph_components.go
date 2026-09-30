package indexer

func dependencyComponents(count int, neighbors func(int) []int) [][]int {
	index := make([]int, count)
	low := make([]int, count)
	onStack := make([]bool, count)
	stack := []int{}
	order := 0
	groups := [][]int{}
	var visit func(int)
	visit = func(node int) {
		order++
		index[node], low[node] = order, order
		stack = append(stack, node)
		onStack[node] = true
		for _, target := range neighbors(node) {
			if index[target] == 0 {
				visit(target)
				low[node] = min(low[node], low[target])
			} else if onStack[target] {
				low[node] = min(low[node], index[target])
			}
		}
		if low[node] != index[node] {
			return
		}
		group := []int{}
		for {
			last := len(stack) - 1
			member := stack[last]
			stack = stack[:last]
			onStack[member] = false
			group = append(group, member)
			if member == node {
				break
			}
		}
		groups = append(groups, group)
	}
	for node := 0; node < count; node++ {
		if index[node] == 0 {
			visit(node)
		}
	}
	return groups
}
