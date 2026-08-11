// SPDX-License-Identifier: GPL-3.0-or-later

package cdccontext

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/HK47196/zill/internal/gamefmt/cdc"
)

const (
	noFlowNode    = -1
	unknownChoice = -2
)

type flowNodeKind uint8

const (
	flowCommand flowNodeKind = iota
	flowLabel
	flowReturn
	flowEnd
	flowBranch
	flowJoin
)

type flowNode struct {
	kind    flowNodeKind
	offset  int
	raw     string
	command cdc.Command
	label   string
	target  int
	next    []int
	owner   cdc.Command
	path    []int
	guard   string
	depth   int
}

type flowGraph struct {
	nodes        []flowNode
	entry        int
	labelTargets map[string][]labelTarget
}

type labelTarget struct {
	node int
	path []int
}

type flowCompiler struct {
	graph flowGraph
}

func compileFlow(program cdc.Program) (flowGraph, error) {
	compiler := flowCompiler{graph: flowGraph{entry: noFlowNode, labelTargets: make(map[string][]labelTarget)}}
	compiler.graph.entry = compiler.sequence(program.Elements, nil, "", 0, noFlowNode)
	for index := range compiler.graph.nodes {
		node := &compiler.graph.nodes[index]
		if node.kind != flowCommand || (node.command.Name != "C69" && node.command.Name != "C70") {
			continue
		}
		if len(node.command.Arguments) != 1 {
			return flowGraph{}, fmt.Errorf("%s@%d: malformed label target", node.command.Name, node.offset)
		}
		target, found, ambiguous := nearestLabelTarget(compiler.graph.labelTargets[node.command.Arguments[0]], node.path)
		if !found {
			return flowGraph{}, fmt.Errorf("%s@%d: missing label %s", node.command.Name, node.offset, node.command.Arguments[0])
		}
		if ambiguous {
			return flowGraph{}, fmt.Errorf("%s@%d: ambiguous label %s", node.command.Name, node.offset, node.command.Arguments[0])
		}
		node.target = target
	}
	return compiler.graph, nil
}

func (compiler *flowCompiler) sequence(elements []cdc.Element, path []int, guard string, depth, continuation int) int {
	var build func(int) int
	build = func(index int) int {
		if index >= len(elements) {
			return continuation
		}
		element := elements[index]
		if element.Kind == cdc.LabelElement {
			if index+1 < len(elements) && elements[index+1].Kind == cdc.BlockElement {
				rest := build(index + 2)
				block := elements[index+1]
				childPath := appendPath(path, index+1)
				childGuard := appendGuard(guard, element.Raw)
				body := compiler.sequence(block.Block.Elements, childPath, childGuard, depth+1, rest)
				compiler.addLabelTarget(element.Label, body, path)
				return compiler.add(flowNode{
					kind: flowLabel, offset: element.Offset, raw: element.Raw,
					label: element.Label, next: successors(rest), path: clonePath(path), guard: guard, depth: depth,
				})
			}
			rest := build(index + 1)
			compiler.addLabelTarget(element.Label, rest, path)
			// Lexically encountered labels define skipped bodies. C69/C70 target
			// the stream immediately after the label instead.
			return compiler.add(flowNode{
				kind: flowLabel, offset: element.Offset, raw: element.Raw,
				label: element.Label, path: clonePath(path), guard: guard, depth: depth,
			})
		}

		rest := build(index + 1)
		switch element.Kind {
		case cdc.CommandElement:
			return compiler.add(flowNode{
				kind: flowCommand, offset: element.Offset, raw: element.Raw, command: element.Command,
				target: noFlowNode, next: successors(rest), path: clonePath(path), guard: guard, depth: depth,
			})
		case cdc.BlockElement:
			owner := precedingCommand(elements, index)
			childPath := appendPath(path, index)
			childGuard := appendGuard(guard, precedingRaw(elements, index, element.Raw))
			join := compiler.add(flowNode{
				kind: flowJoin, offset: element.Block.CloseOffset, raw: element.Block.CloseRaw,
				next: successors(rest), path: clonePath(path), guard: guard, depth: depth,
			})
			body := compiler.sequence(element.Block.Elements, childPath, childGuard, depth+1, join)
			return compiler.add(flowNode{
				kind: flowBranch, offset: element.Offset, raw: element.Raw, owner: owner,
				next: branchSuccessors(body, join), path: clonePath(path), guard: guard, depth: depth,
			})
		case cdc.ReturnElement:
			return compiler.add(flowNode{
				kind: flowReturn, offset: element.Offset, raw: element.Raw,
				path: clonePath(path), guard: guard, depth: depth,
			})
		case cdc.EndElement:
			return compiler.add(flowNode{
				kind: flowEnd, offset: element.Offset, raw: element.Raw,
				path: clonePath(path), guard: guard, depth: depth,
			})
		}
		return rest
	}
	return build(0)
}

func (compiler *flowCompiler) add(node flowNode) int {
	index := len(compiler.graph.nodes)
	compiler.graph.nodes = append(compiler.graph.nodes, node)
	return index
}

func (compiler *flowCompiler) addLabelTarget(label string, target int, path []int) {
	compiler.graph.labelTargets[label] = append(compiler.graph.labelTargets[label], labelTarget{node: target, path: clonePath(path)})
}

func nearestLabelTarget(candidates []labelTarget, path []int) (int, bool, bool) {
	bestNode, bestDepth, matches := noFlowNode, -1, 0
	for _, candidate := range candidates {
		if !pathPrefix(candidate.path, path) {
			continue
		}
		depth := len(candidate.path)
		if depth > bestDepth {
			bestNode, bestDepth, matches = candidate.node, depth, 1
		} else if depth == bestDepth {
			matches++
		}
	}
	return bestNode, bestNode != noFlowNode, matches > 1
}

func pathPrefix(prefix, path []int) bool {
	if len(prefix) > len(path) {
		return false
	}
	for index := range prefix {
		if prefix[index] != path[index] {
			return false
		}
	}
	return true
}

func successors(node int) []int {
	if node == noFlowNode {
		return nil
	}
	return []int{node}
}

func branchSuccessors(body, skipped int) []int {
	result := make([]int, 0, 2)
	if body != noFlowNode {
		result = append(result, body)
	}
	if skipped != noFlowNode && skipped != body {
		result = append(result, skipped)
	}
	return result
}

func precedingCommand(elements []cdc.Element, index int) cdc.Command {
	if index > 0 && elements[index-1].Kind == cdc.CommandElement {
		return elements[index-1].Command
	}
	return cdc.Command{}
}

func precedingRaw(elements []cdc.Element, index int, fallback string) string {
	if index == 0 {
		return fallback
	}
	previous := elements[index-1]
	if previous.Raw != "" {
		return previous.Raw
	}
	if previous.Kind == cdc.CommandElement {
		return rawCommand(previous.Command)
	}
	return fallback
}

func appendPath(path []int, component int) []int {
	result := make([]int, len(path)+1)
	copy(result, path)
	result[len(path)] = component
	return result
}

func clonePath(path []int) []int {
	return append([]int{}, path...)
}

func appendGuard(guard, owner string) string {
	if guard == "" {
		return owner
	}
	return guard + " > " + owner
}

type actorFact struct {
	presence string
	basis    string
}

type actorState map[int]actorFact

type abstractFlow struct {
	actors      actorState
	supported   bool
	unsupported bool
}

type flowContext struct {
	node       int
	returnNode int
	choice     int
}

type flowAnalysis struct {
	byNode map[int]abstractFlow
}

func analyzeFlow(graph flowGraph) flowAnalysis {
	analysis := flowAnalysis{byNode: make(map[int]abstractFlow)}
	if graph.entry == noFlowNode {
		return analysis
	}
	states := make(map[flowContext]abstractFlow)
	queued := make(map[flowContext]bool)
	queue := make([]flowContext, 0)
	enqueue := func(context flowContext, incoming abstractFlow) {
		if context.node == noFlowNode {
			return
		}
		current, exists := states[context]
		if !exists {
			states[context] = incoming.clone()
		} else {
			joined, changed := joinFlow(current, incoming)
			if !changed {
				return
			}
			states[context] = joined
		}
		if !queued[context] {
			queued[context] = true
			queue = append(queue, context)
		}
	}
	enqueue(flowContext{node: graph.entry, returnNode: noFlowNode, choice: unknownChoice}, abstractFlow{actors: make(actorState), supported: true})
	for len(queue) > 0 {
		context := queue[0]
		queue = queue[1:]
		queued[context] = false
		incoming := states[context]
		node := graph.nodes[context.node]
		outgoing := incoming.clone()
		if node.kind == flowCommand {
			applyLifecycle(&outgoing, node.command)
		}
		switch node.kind {
		case flowCommand:
			switch node.command.Name {
			case "C69":
				enqueue(flowContext{node: node.target, returnNode: noFlowNode, choice: context.choice}, outgoing)
			case "C70":
				if context.returnNode != noFlowNode {
					outgoing = outgoing.asUnsupported()
				}
				enqueue(flowContext{node: node.target, returnNode: firstSuccessor(node), choice: context.choice}, outgoing)
			case "C71":
				if context.returnNode != noFlowNode {
					enqueue(flowContext{node: context.returnNode, returnNode: noFlowNode, choice: context.choice}, outgoing)
				} else {
					forward(graph, node, flowContext{returnNode: noFlowNode, choice: context.choice}, outgoing.asUnsupported(), enqueue)
				}
			case "C20":
				count, ok := choiceCount(node.command)
				if !ok {
					forward(graph, node, flowContext{returnNode: context.returnNode, choice: unknownChoice}, outgoing.asUnsupported(), enqueue)
					break
				}
				for choice := -1; choice < count; choice++ {
					forward(graph, node, flowContext{returnNode: context.returnNode, choice: choice}, outgoing, enqueue)
				}
			default:
				forward(graph, node, flowContext{returnNode: context.returnNode, choice: context.choice}, outgoing, enqueue)
			}
		case flowBranch:
			branch(graph, node, context, outgoing, enqueue)
		case flowReturn:
			if context.returnNode != noFlowNode {
				enqueue(flowContext{node: context.returnNode, returnNode: noFlowNode, choice: context.choice}, outgoing.asUnsupported())
			}
		case flowLabel, flowJoin:
			forward(graph, node, flowContext{returnNode: context.returnNode, choice: context.choice}, outgoing, enqueue)
		case flowEnd:
		}
	}
	for context, state := range states {
		current, exists := analysis.byNode[context.node]
		if !exists {
			analysis.byNode[context.node] = state.clone()
			continue
		}
		joined, _ := joinFlow(current, state)
		analysis.byNode[context.node] = joined
	}
	return analysis
}

func forward(graph flowGraph, node flowNode, context flowContext, state abstractFlow, enqueue func(flowContext, abstractFlow)) {
	for _, next := range node.next {
		context.node = next
		enqueue(context, state)
	}
}

func branch(graph flowGraph, node flowNode, context flowContext, state abstractFlow, enqueue func(flowContext, abstractFlow)) {
	if len(node.next) == 0 {
		return
	}
	if node.owner.Name == "C20" {
		enqueue(flowContext{node: node.next[0], returnNode: context.returnNode, choice: context.choice}, state)
		return
	}
	if node.owner.Name == "C21" && context.choice != unknownChoice {
		want, err := strconv.Atoi(firstArgument(node.owner))
		if err == nil {
			next := 0
			if context.choice != want && len(node.next) > 1 {
				next = 1
			}
			enqueue(flowContext{node: node.next[next], returnNode: context.returnNode, choice: context.choice}, state)
			return
		}
	}
	for _, next := range node.next {
		enqueue(flowContext{node: next, returnNode: context.returnNode, choice: context.choice}, state)
	}
}

func firstArgument(command cdc.Command) string {
	if len(command.Arguments) == 0 {
		return ""
	}
	return command.Arguments[0]
}

func firstSuccessor(node flowNode) int {
	if len(node.next) == 0 {
		return noFlowNode
	}
	return node.next[0]
}

func choiceCount(command cdc.Command) (int, bool) {
	if command.Name != "C20" || len(command.Arguments) < 2 {
		return 0, false
	}
	count, err := strconv.Atoi(command.Arguments[1])
	return count, err == nil && count >= 1 && count <= 37
}

func applyLifecycle(flow *abstractFlow, command cdc.Command) {
	handle, ok := firstInt(command)
	if !ok {
		return
	}
	switch command.Name {
	case "C2":
		flow.actors[handle] = actorFact{presence: "present", basis: "cfg_lifecycle"}
	case "C6":
		if current, exists := flow.actors[handle]; exists {
			if current.presence == "present" || current.basis == "state_disagreement" {
				return
			}
		}
		flow.actors[handle] = actorFact{presence: "unknown", basis: "insufficient_lifecycle_evidence"}
	case "C3":
		flow.actors[handle] = actorFact{presence: "absent", basis: "cfg_lifecycle"}
	}
}

func (flow abstractFlow) clone() abstractFlow {
	return abstractFlow{actors: flow.actors.clone(), supported: flow.supported, unsupported: flow.unsupported}
}

func (flow abstractFlow) asUnsupported() abstractFlow {
	flow.supported = false
	flow.unsupported = true
	return flow
}

func (state actorState) clone() actorState {
	result := make(actorState, len(state))
	for handle, fact := range state {
		result[handle] = fact
	}
	return result
}

func joinFlow(left, right abstractFlow) (abstractFlow, bool) {
	result := abstractFlow{
		actors:      joinActors(left.actors, right.actors),
		supported:   left.supported || right.supported,
		unsupported: left.unsupported || right.unsupported,
	}
	return result, !flowEqual(left, result)
}

func joinActors(left, right actorState) actorState {
	handles := make([]int, 0, len(left)+len(right))
	seen := make(map[int]bool, len(left)+len(right))
	for handle := range left {
		seen[handle] = true
		handles = append(handles, handle)
	}
	for handle := range right {
		if !seen[handle] {
			handles = append(handles, handle)
		}
	}
	result := make(actorState, len(handles))
	for _, handle := range handles {
		leftFact, leftExists := left[handle]
		rightFact, rightExists := right[handle]
		switch {
		case !leftExists || !rightExists:
			result[handle] = actorFact{presence: "unknown", basis: "state_disagreement"}
		case leftFact.presence != rightFact.presence:
			result[handle] = actorFact{presence: "unknown", basis: "state_disagreement"}
		case leftFact.presence == "unknown":
			basis := "insufficient_lifecycle_evidence"
			if leftFact.basis == "state_disagreement" || rightFact.basis == "state_disagreement" || leftFact.basis != rightFact.basis {
				basis = "state_disagreement"
			}
			result[handle] = actorFact{presence: "unknown", basis: basis}
		default:
			result[handle] = actorFact{presence: leftFact.presence, basis: "cfg_lifecycle"}
		}
	}
	return result
}

func flowEqual(left, right abstractFlow) bool {
	if left.supported != right.supported || left.unsupported != right.unsupported || len(left.actors) != len(right.actors) {
		return false
	}
	for handle, fact := range left.actors {
		if right.actors[handle] != fact {
			return false
		}
	}
	return true
}

func (flow abstractFlow) reachability() string {
	switch {
	case flow.supported && flow.unsupported:
		return "mixed"
	case flow.supported:
		return "supported"
	case flow.unsupported:
		return "unsupported"
	default:
		return "unreachable"
	}
}

func sourceOrderedNodes(graph flowGraph) []int {
	indexes := make([]int, len(graph.nodes))
	for index := range graph.nodes {
		indexes[index] = index
	}
	slices.SortStableFunc(indexes, func(left, right int) int {
		return graph.nodes[left].offset - graph.nodes[right].offset
	})
	return indexes
}
