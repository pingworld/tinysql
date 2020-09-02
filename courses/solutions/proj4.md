## 题解

### 心路

刚看到这个题目的时候，对优化器只是耳闻，对其执行原理一无所知。所以我老老实实的看了作业的说明，以及参考这里：https://pingcap.com/blog-cn/tidb-cascades-planner/， 还有这里：https://pingcap.com/blog-cn/tidb-source-code-reading-7/， 这里：https://pingcap.com/blog-cn/tidb-source-code-reading-21/， 这里： https://pingcap.com/blog-cn/tidb-source-code-reading-6/ 和
https://pingcap.com/blog-cn/tidb-source-code-reading-7/。 

看完就可以做作业了吗？so simple sometimes naive！

那怎么办？还是要回过头仔细看源码，对着TinySQL把一条SQL从头到尾的执行流程过一遍，主要是将作业的说明对应到代码上，有个大概的感性认知。

其次，在看的时候，我觉得最重要的就是数据结构，要将抽象的查询计划在头脑中形成一个形象，这样做作业的时候，思路会有着力点。

比如：Plan分为LogicalPlan（逻辑查询计划）和PhysicalPlan（物理查询计划），它长什么样子呢？你可以通过`planner/core/stringer.go:ToString(plan)`来输出其表示形式：
```golang
// LogicalPlan

// The original SQL.
# select * from t t1 join t t2 on t1.a = t2.a where t2.a = null #

// Logcial plan generated from ast.stmtNode before optimizing.
//          Projection
//              |
//          Sel(t2.a = null)
//              |
//              Join
//             /   \
//  DataScan(t1)    DataScan(t2)
// The tree of logical operators.
+ Join{DataScan(t1)->DataScan(t2)}(test.t.a,test.t.a)->Sel([eq(test.t.a, <nil>)])->Projection +

// PhysicalPlan

// The original SQL.
# insert into t select * from t where b < 1 order by d limit 1 #

// The tree of physical operators.
+ TableReader(Table(t)->Sel([lt(test.t.b, 1)])->TopN([test.t.d],0,1))->TopN([test.t.d],0,1)->Insert +
```

需要有所印象的结构我觉得有这些：
```golang
ast.Node

expression.Expression

LogicalAggregation
```

上面介绍了基于规则的优化，但还有一种优化框架Cascades，其启用方法参考这里：
```golang
if sctx.GetSessionVars().EnableCascadesPlanner {
	finalPlan, err := cascades.DefaultOptimizer.FindBestPlan(sctx, logic)
	return finalPlan, names, err
}
finalPlan, err := plannercore.DoOptimize(ctx, builder.GetOptFlag(), logic)
```

根据这里`(opt *Optimizer) FindBestPlan`的注释，可以知道Cascades优化器会分为3个阶段：预处理（Preprocessing，启发式规则）、逻辑搜索（Exploration，对逻辑算子树做逻辑上的等价变换）、实现（Implementation，类似将逻辑计划转变为物理计划）。

Cascades中有几个概念需要描述下：
	- memo：看这个名字，大概也能猜出一二，用于存放搜索的解空间，各种等价的算子树
	- group：用于存放所有逻辑上等价的表达式，是GroupExpr的集合。
```golang
	type Group struct {
		Equivalents *list.List
	
		FirstExpr    map[Operand]*list.Element
		Fingerprints map[string]*list.Element
	
		Explored        bool
		SelfFingerprint string
	
		ImplMap map[string]Implementation
		Prop    *property.LogicalProperty
	
		EngineType EngineType
	
		//hasBuiltKeyInfo indicates whether this group has called `BuildKeyInfo`.
		// BuildKeyInfo is lazily called when a rule needs information of
		// unique key or maxOneRow (in LogicalProp). For each Group, we only need
		// to collect these information once.
		hasBuiltKeyInfo bool
    }
```
	- group expression：存放具有相同根节点下的所有在逻辑上等价的表达式。与普通的表达式不同的，GroupExpr的子节点称之为Group而非Expression。另外，一旦设置了子Group就不再发生变化了。
```golang
	type GroupExpr struct {
		ExprNode plannercore.LogicalPlan
		Children []*Group
		Explored bool
		Group    *Group
	
		selfFingerprint string
    }
```
	- rule：规则，分为两种，Transformation Rule，用于增加等价的group expression，以便扩展memo中的解空间，名字也比较形象，可理解为等价变换。Implementation Rule，用于为group expresssion选择物理算子。
	- pattern：规则的匹配逻辑，树形结构，每个节点由逻辑表达式以及执行引擎来决定。
```golang
	type Pattern struct {
		Operand
		EngineTypeSet
		Children []*Pattern
	}
```	
	- Transformation Rule，一个接口，定义变换的规则
```golang
	type Transformation interface {
		// GetPattern gets the cached pattern of the rule.
		GetPattern() *memo.Pattern
		
		// 由于 Pattern 只能描述算子的类型，不能描述 LogicalPlan 内部的内容约束，因此通过 Match() 方法可以判断更细节的匹配条件。例如 Pattern 只能描述我们想要一个 Join 类型的算子，但是却没法描述这个 Join 应该是 InnerJoin 或者是 LeftOuterJoin，这类条件就需要在 Match() 中进行判断。
		Match(expr *memo.ExprIter) bool
		
		// 输入：待优化的GroupExpre
		// 输出：优化后的GroupExpre
		OnTransform(old *memo.ExprIter) (newExprs []*memo.GroupExpr, eraseOld bool, eraseAll bool, err error)
	}
```
	每个Transformation Rule都有自己的pattern，用于转换符合该pattern的Expression。
	- ExprIter：枚举所有等价的Group
	- Implementation Rule：将一个逻辑算子转换成物理算子的规则。

比如说，这次的作业：
PushSelDownAggregation是一个Transformation，它实现了`Selection -> Aggregation`的下推转换。下推的原理很简单，就是尽可能的将过滤条件接近数据源（DataSource）。其原理跟core这个packge的作业一样。

思考至此，有一个疑问：优化器框架为何被称之为一种搜索算法？

我的理解是，所谓优化那必然是要找到最优，但还必须要保证逻辑上的等价。这就意味着要从多个不同选择中找到一个代价最小的，所以这就存在一个解空间（所有等价的查询计划组成）和最优解（代价最小的查询计划）以及一套搜索算法，即这里的优化器框架所提供的逻辑。

以上就是查询计划相关的作业了。第2部分跟优化有一定的关系，首先介绍了优化的基准 —— 代价统计，其次介绍了两种场景下的优化：
- 关系Join时的重排序
- 索引选择时的优化

代价的统计类似布隆过滤器，理解了原理之后，实现起来还是较为简单。

JoinReorder涉及到动态规划，为了做这个作业，我费了不少劲，因为把之前动态规划的算法思想给忘得差不多了，所以又从最基本的动规算法学习，然后又学了dpsub的原理，最后参考了这里：https://github.com/winoros/DP-CCP/blob/dp-sub/dpsub.cpp 才算完成了作业。。

动规的本质是一种迭代，即不断的通过更小的问题来解决更大的问题。所以，动规的两个先决条件就是：初始状态和状态变更函数。我是用经典的硬币问题来熟悉动规的。

就当前的作业来说，Join关系的重排，无非就是找到合适的顺序。初始状态就是只有两个关系，那不用重排。每当增加一个关系后，根据计算新增后的关系集合的排序，找到最小代价。以此类推，直至找到所有待重排关系集合的最小代价。最后，我还参考了这里： https://www.cockroachlabs.com/blog/join-ordering-pt1/ 才算对重排的物理意义有了直观上的认识。

关于访问路径优化，我是严格参考这里： https://github.com/pingcap/tidb/blob/master/docs/design/2019-01-25-skyline-pruning.md 实现的。简单翻一下：

访问路径的选择高度依赖统计数据。但由于统计可能会过期导致选择的索引失效。但有些很明显的经验法则可以用于避免这类情况，比如当匹配主键或者唯一键索引时，就可以直接用这类索引而不再考虑统计信息。

所以，选择访问路径的最重要的因素就是扫描的行数，是否匹配物理属性，以及是否需要多次扫描。这3类因素中只有行数依赖于统计。所以，我们如何在没有统计信息的时候比对扫描行。如下面这个例子：

create table t (a int, b int, c int, index idx1(b, a), index idx2(a));

select * from t where a = 1 and b = 1;

比如有2个路径，x、y，如果在多个因素上，x不必y坏，但只要有一点x比y好，就可以不用计算统计信息就将y给排除在外，这是因为x的表现肯定要比y好。该算法称之为skyline pruning。

当看过这些内容之后，慢慢的对作业就会有一定的亲切感，加些日志基本上就能上手了。当然这只是我这种新手的方式，不一定适用于大家。所以仅供参考。

### 实现

#### 4-1-1

根据这里的评论：
```goalng
//       A simple example is that
//       `select * from (select count(*) from t group by b) tmp_t where b > 1` is the same with
//       `select * from (select count(*) from t where b > 1 group by b) tmp_t.
```

大体上能猜到，就是是否可以将`where b > 1`这一谓词过滤条件下推到`group by`之前，优先过滤，这样可以尽可能的减少聚集所需要操作的数据量。

其实，这是一种逻辑上查询变换，所以这也称之为基于规则的优化，只要规则上等价即可。

具体的实现可参考源码的注释 —— 简单说就是过滤条件的所有列必须处于group by中，否则可能会导致结果不一致。

#### 4-1-2

原理较简单，其依据跟4-1类似，所以我这里将两者通用的算法给做成了一个公共函数。具体可参考代码中的注释。

#### 4-2-1

参考布隆过滤器的原理 —— 略

#### 4-2-2

目标：当前有参与Join运算的关系集合，如何排列才能得到最优的整体查询代价。

原理：Join符合交换律，所以适当的重排关系可能会得到更好的查询计划。

流程：
1. 基于过滤条件构建用于重排的节点空间，存在等值条件的节点之间Join，如果适当变更排序可能会带来更小的代价。
2. 针对所有节点组成的子集找到其最优组合，比如f[6] = f[2] + f[4] || f[3] + f[3]，哪个好？需要判断
3. 当所有的子节点都是最优的话，那假定组合起来的代价也是最优的 —— 这一点不一定成立，但也只能这样了。

具体实现参考源码中的注释。

#### 4-2-3

在三要素上比较好坏：

```golang
func ifSetOfColumnsInTheAccessCondition(lcs, rcs *intsets.Sparse) (int, bool)

func cmpIfMatchPhysicalProp(lmp, rmp bool) int

func cmpIfRequireDoubleScan(lss, rss bool) int
```

然后参考这里的注释：
```golang
// compareCandidates is the core of skyline pruning. It compares the two candidate paths on three dimensions:
// (1): the set of columns that occurred in the access condition,
// (2): whether or not it matches the physical property
// (3): does it require a double scan.
// If `x` is not worse than `y` at all factors,
// and there exists one factor that `x` is better than `y`, then `x` is better than `y`.
// x比y好，必须满足：
// 1. 在所有因素上，x不必y差
// 2. 存在某个因素，x要比y好
```

## 感受

做完这个project之后，有两点感受：
- 理论确实有其价值，这里的很多点都有具体的paper支撑 —— 第一次感受到paper跟自己如此的近；

- 从未知到已知，这个过程很耗费精力，从已知到熟知，这个过程可能要稍微好一点，从熟知到精通，这个过程也会很费精力，可能很少人能走到精通的地方。但这个并不一定所有人都需要；

