## 题解

### 心路

我发现当经历过proj4的折磨之后，再看这里的章节有一种透彻的感觉。。。当然，很多具体的知识点还是有些不清不楚。

有一些结构体还是没有形象化。

比如：chunk，所以我参考了这里： https://pingcap.com/blog-cn/tidb-source-code-reading-10/ ，以及手动解析了下。

另外，其实看了一堆的官方理论介绍，也没有形成一个直观的印象，所以我还是准备用自己的话解释下，何为执行器？

经过前面内容的洗礼，一条SQL到本节介绍的执行器这里应该就是一个个算子组成的查询树了，那这个树所描述的查询如何才能切实的从数据存储中获取到数据呢？带着这个问题，我看到了这里： https://docs.pingcap.com/zh/tidb/stable/query-execution-plan ，非常直观的回答了这个问题。

所以，执行器本质上就是一个数据获取的流程，根据查询树这一指导原则，执行具体的算子，包括获取原始数据、过滤数据以及算子之间共享数据等等。最终得到所需要的最后结果。

在我通过日志分析的过程中，有一点值得注意就是，执行器返回给外部的并不是一个直接的数据集合，而是一个迭代器，如果想要所有结果集合，必须遍历该迭代器，比如这里：
```golang
GetRows4Test(ctx context.Context, sctx sessionctx.Context, rs sqlexec.RecordSet)

// Must reuse `req` for imitating server.(*clientConn).writeChunks
for {
    err := rs.Next(ctx, req)
    if err != nil {
        return nil, err
    }
    if req.NumRows() == 0 {
        break
    }

    iter := chunk.NewIterator4Chunk(req.CopyConstruct())
    for row := iter.Begin(); row != iter.End(); row = iter.Next() {
        rows = append(rows, row)
    }
}
```

### 实现

#### 5-1 向量化表达式

对比下这两个实现：
builtin_string_vec.go:builtinLengthSig:vecEvalInt(input *chunk.Chunk, result *chunk.Column)
builtin_string.go:builtinLengthSig:evalInt(row chunk.Row)

本质上，两者都是计算字符串长度的。只是后者（非向量模式）只会计算单一字符串的长度。而前者（向量化）会一并计算N个字符串的长度。

另外，还要记得实现这里的缺失逻辑：
executor/executor.go:(e *SelectionExec) Next(ctx context.Context, req *chunk.Chunk)
其目的是为了检查结果中的列是否被选中。

我觉得5-1中的测试似乎应该放到5-2中比较合适。

#### 5-2 HashJoinExec

这节的作业说的挺仔细了，先构建inner table，然后再并发的probe另一个table。效率的提升就体现在这里的并发probe上。

#### 5-3 HashAgg

作业上的解释也比较明确，不再赘述。
1. 启动 Data Fetcher，Partial Workers 及 Final Workers

	这部分工作由 prepare4ParallelExec 函数完成。该函数会启动一个 Data Fetcher，多个 Partial Worker 以及多个 Final Worker。
	
2. DataFetcher 读取子节点的数据并分发给 Partial Workers
	
	这部分工作由 fetchChildData 函数完成。
	
3. Partial Workers 预聚合计算，及根据 Group Key shuffle 给对应的 Final Workers

	这部分工作由 HashAggPartialWorker.run 函数完成。该函数调用 updatePartialResult 函数对 DataFetcher 发来数据执行预聚合计算，并将预聚合结果存储到 partialResultMap 中。其中 partialResultMap 的 key 为根据 Group-By 的值 encode 的结果，value 为 PartialResult 类型的数组，数组中的每个元素表示该下标处的聚合函数在对应 Group 中的预聚合结果。shuffleIntermData 函数完成根据 Group 值 shuffle 给对应的 Final Worker。

4. Final Worker 计算最终结果，发送给 Main Thread

	这部分工作由 HashAggFinalWorker.run 函数完成。该函数调用 consumeIntermData 函数接收 PartialWorkers 发送来的预聚合结果，进而合并得到最终结果。getFinalResult 函数完成发送最终结果给 Main Thread。

5. Main Thread 接收最终结果并返回
