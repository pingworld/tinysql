// Copyright 2018 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package core

import (
	"math/bits"

	"github.com/pingcap/tidb/expression"
	"github.com/pingcap/tidb/parser/ast"
)

type joinReorderDPSolver struct {
	*baseSingleGroupJoinOrderSolver
	newJoin func(lChild, rChild LogicalPlan, eqConds []*expression.ScalarFunction, otherConds []expression.Expression) LogicalPlan
}

type joinGroupEqEdge struct {
	nodeIDs []int
	edge    *expression.ScalarFunction
}

type joinGroupNonEqEdge struct {
	nodeIDs    []int
	nodeIDMask uint
	expr       expression.Expression
}

// joinGroup 参与Join运算的数据源
// eqConds 作用到数据源的条件表达式
//
// 使用数字的二进制表示来代表当前参与 Join 的节点情况。11（二进制表示为 1011）表示当前的 Join Group 包含了第 3 号节点，第 1 号节点和第 0 号节点（节点从 0 开始计数）。
// f[11] 来表示包含了节点 3, 1, 0 的最优的 Join Tree。
// 转移方程则是 f[group] = min{join{f[sub], f[group ^ sub])} 这里 sub 是 group 二进制表示下的任意子集。
func (s *joinReorderDPSolver) solve(joinGroup []LogicalPlan, eqConds []expression.Expression) (LogicalPlan, error) {
	// TODO: You need to implement the join reorder algo based on DP.

	// The pseudo code can be found in README.
	// And there's some common struct and method like `baseNodeCumCost`, `calcJoinCumCost` you can use in `rule_join_reorder.go`.
	// Also, you can take a look at `rule_join_reorder_greedy.go`, this file implement the join reorder algo based on greedy algorithm.
	// You'll see some common usages in the greedy version.

	// Note that the join tree may be disconnected. i.e. You need to consider the case `select * from t, t1, t2`.
	// 即没有关系的表之间join

	/*
		目标：当前有参与Join运算的关系集合，如何排列才能得到最优的整体查询代价。

		原理：Join符合交换律，所以适当的重排关系可能会得到更好的查询计划。

		流程：
		1. 基于过滤条件构建用于重排的节点空间，存在等值条件的节点之间Join，如果适当变更排序可能会带来更小的代价。
		2. 针对所有节点组成的子集找到其最优组合，比如f[6] = f[2] + f[4] || f[3] + f[3]，哪个好？需要判断
		3. 当所有的子节点都是最优的话，那假定组合起来的代价也是最优的 —— 这一点不一定成立，但也只能这样了。
	*/

	// 初始化
	for _, jg := range joinGroup {
		jr := &jrNode{
			p:       jg,
			cumCost: s.baseNodeCumCost(jg),
		}

		s.curJoinGroup = append(s.curJoinGroup, jr)
	}

	edges := make([][]int, len(joinGroup))
	allEqEdges := make([]joinGroupEqEdge, 0, len(joinGroup))

	for _, cond := range eqConds {
		sf := cond.(*expression.ScalarFunction)

		// 等值查询的左右关系
		lCol := sf.GetArgs()[0].(*expression.Column)
		rCol := sf.GetArgs()[1].(*expression.Column)

		// 通过列查找对应的关系，及其在JoinGroup中的下标
		lIdx, err := findNodeIndexInGroup(joinGroup, lCol)
		if err != nil {
			return nil, err
		}
		rIdx, err := findNodeIndexInGroup(joinGroup, rCol)
		if err != nil {
			return nil, err
		}

		edges[lIdx] = append(edges[lIdx], rIdx)
		edges[rIdx] = append(edges[rIdx], lIdx)

		allEqEdges = append(allEqEdges, joinGroupEqEdge{
			nodeIDs: []int{lIdx, rIdx},
			edge:    sf,
		})
	}

	var joins []LogicalPlan
	connectedRels := make([]bool, len(joinGroup))

	for i := 0; i < len(joinGroup); i++ {
		if connectedRels[i] {
			continue
		}

		// 查找当前关系连通的子关系集合
		subGroup2OriginGroup := s.findConnectedSubGraphs(i, connectedRels, edges)

		join, err := s.dpSub(subGroup2OriginGroup, allEqEdges)
		if err != nil {
			return nil, err
		}

		joins = append(joins, join)
	}

	return s.makeBushyJoin(joins, nil), nil
}

// 广度遍历，查找有等值条件关联的关系集合，称之为连通关系集
func (s *joinReorderDPSolver) findConnectedSubGraphs(relIdx int, connectedRels []bool, edges [][]int) (subGroup2OriginGroup map[int]int) {
	subGroup2OriginGroup = make(map[int]int)

	queue := []int{relIdx}

	connectedRels[relIdx] = true

	i := 0

	for len(queue) > 0 {
		curNodeID := queue[0]
		queue = queue[1:]

		subGroup2OriginGroup[i] = curNodeID
		i += 1

		for _, adjNodeID := range edges[curNodeID] {
			if connectedRels[adjNodeID] {
				continue
			}

			queue = append(queue, adjNodeID)

			connectedRels[adjNodeID] = true
		}
	}

	return
}

// 原理
// 针对每个关系子集，找到其最佳的排序
func (s *joinReorderDPSolver) dpSub(subGroup2OriginGroup map[int]int, allEqEdges []joinGroupEqEdge) (LogicalPlan, error) {
	relationCount := len(subGroup2OriginGroup)

	dp := make([]*jrNode, 1<<relationCount)
	for i, g := range subGroup2OriginGroup {
		dp[1<<i] = s.curJoinGroup[g]
	}

	// state可认为一个需要重排顺序的关系集合，拆分重组然后判断是否更优，如果是则按新的方式组合得到一个新的Join计划。
	for state := uint(1); state < 1<<relationCount; state++ {
		// 只有一个关系的话显然是不需要重排的
		if bits.OnesCount(state) == 1 {
			continue
		}

		// 多个关系的处理
		for sub := (state - 1) & state; sub > 0; sub = (sub - 1) & state {
			remain := state ^ sub

			// 从计算代价的角度来看，f[1,3] <=> f[3,1]，所以只需要判断一种顺序即可
			if sub > remain {
				continue
			}

			if dp[sub] == nil || dp[remain] == nil {
				continue
			}

			// 当两个关系集合中，存在着等值判断的列时，则意味着这两个关系集合可以通过这个等值条件重新组合Join
			usedEdges := s.areRelationsConnected(sub, remain, subGroup2OriginGroup, allEqEdges)
			if len(usedEdges) == 0 {
				// 只处理用等值关联的关系
				continue
			}

			// 因为sub和remain所代表的查询计划有等值关系连接，所以将其组合成一个新的Join查询计划
			// 此时，state对应的组合方式已经有了新的选择，所以需要对比是否要比原来的代价更低
			newJoin, err := s.newJoinWithEdge(dp[sub].p, dp[remain].p, usedEdges, nil)
			if err != nil {
				return nil, err
			}

			// 计算该查询计划的代价 —— 主要就是涉及到的行数
			curCost := s.calcJoinCumCost(newJoin, dp[sub], dp[remain])
			if dp[state] == nil {
				dp[state] = &jrNode{
					p:       newJoin,
					cumCost: curCost,
				}
			} else if curCost < dp[state].cumCost {
				// 新组合的Join的代价要比原始更小，则替换原来的Join
				dp[state].p = newJoin
				dp[state].cumCost = curCost
			}
		}
	}

	return dp[(1<<relationCount)-1].p, nil
}

func (s *joinReorderDPSolver) areRelationsConnected(sub, remain uint, subGroup2OriginGroup map[int]int, totalEqEdges []joinGroupEqEdge) []joinGroupEqEdge {
	var (
		usedEqEdges []joinGroupEqEdge
	)

	isInRelationGroup := func(group, idx uint) bool {
		return group&(1<<idx) != 0
	}

	for _, edge := range totalEqEdges {
		lIdx := uint(subGroup2OriginGroup[edge.nodeIDs[0]])
		rIdx := uint(subGroup2OriginGroup[edge.nodeIDs[1]])

		// 该列如果存在左右两个join关系集中，则认为两个关系集是连通的
		if (isInRelationGroup(sub, lIdx) && isInRelationGroup(remain, rIdx)) || (isInRelationGroup(remain, lIdx) && isInRelationGroup(sub, rIdx)) {
			usedEqEdges = append(usedEqEdges, edge)
		}
	}

	return usedEqEdges
}

func (s *joinReorderDPSolver) newJoinWithEdge(leftPlan, rightPlan LogicalPlan, edges []joinGroupEqEdge, otherConds []expression.Expression) (LogicalPlan, error) {
	var eqConds []*expression.ScalarFunction
	for _, edge := range edges {
		lCol := edge.edge.GetArgs()[0].(*expression.Column)
		rCol := edge.edge.GetArgs()[1].(*expression.Column)
		if leftPlan.Schema().Contains(lCol) {
			eqConds = append(eqConds, edge.edge)
		} else {
			newSf := expression.NewFunctionInternal(s.ctx, ast.EQ, edge.edge.GetType(), rCol, lCol).(*expression.ScalarFunction)
			eqConds = append(eqConds, newSf)
		}
	}
	join := s.newJoin(leftPlan, rightPlan, eqConds, otherConds)
	_, err := join.recursiveDeriveStats()
	return join, err
}

// Make cartesian join as bushy tree.
func (s *joinReorderDPSolver) makeBushyJoin(cartesianJoinGroup []LogicalPlan, otherConds []expression.Expression) LogicalPlan {
	for len(cartesianJoinGroup) > 1 {
		resultJoinGroup := make([]LogicalPlan, 0, len(cartesianJoinGroup))
		for i := 0; i < len(cartesianJoinGroup); i += 2 {
			if i+1 == len(cartesianJoinGroup) {
				resultJoinGroup = append(resultJoinGroup, cartesianJoinGroup[i])
				break
			}
			// TODO:Since the other condition may involve more than two tables, e.g. t1.a = t2.b+t3.c.
			//  So We'll need a extra stage to deal with it.
			// Currently, we just add it when building cartesianJoinGroup.
			mergedSchema := expression.MergeSchema(cartesianJoinGroup[i].Schema(), cartesianJoinGroup[i+1].Schema())
			var usedOtherConds []expression.Expression
			otherConds, usedOtherConds = expression.FilterOutInPlace(otherConds, func(expr expression.Expression) bool {
				return expression.ExprFromSchema(expr, mergedSchema)
			})
			resultJoinGroup = append(resultJoinGroup, s.newJoin(cartesianJoinGroup[i], cartesianJoinGroup[i+1], nil, usedOtherConds))
		}
		cartesianJoinGroup = resultJoinGroup
	}
	return cartesianJoinGroup[0]
}

func findNodeIndexInGroup(group []LogicalPlan, col *expression.Column) (int, error) {
	for i, plan := range group {
		if plan.Schema().Contains(col) {
			return i, nil
		}
	}
	return -1, ErrUnknownColumn.GenWithStackByArgs(col, "JOIN REORDER RULE")
}
