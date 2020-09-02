## 题解

该项目分成两部分。

1. 第一部分是一个在线答题，主要就是SQL的编写。

    很明显，首先要先会写SQL才有必要学习SQL是如何作用于DB上，处理具体数据的。就本小节来看:

    `Revising the Select Query I (10%) Revising the Select Query II (10%)`，主要就是关于SELECT的简单使用，没啥好说的。 

    `Revising Aggregations - The Count Function (10%) Revising Aggregations - The Sum Function (10%) Revising Aggregations - Averages (10%)`，考察的是聚合函数，这几个函数比较直观，问题不大。

    `Average Population of Each Continent (10%)` 的实现中会用到`GROUP BY`，这可以为后面`proj5`中关于`Hash Aggregate`的练习热下身。

    `African Cities (10%) Binary Tree Nodes (30%)`，相对复杂，会涉及到多表的连接以及一些子查询等。

    要完成这些题目，应该不难，主要就是掌握SQL语法。也有一些参考资料：

    1）《SQL必知必会》，https://book.douban.com/subject/24250054/
    2）MySQL的参考文档，https://dev.mysql.com/doc/refman/5.7/en/sql-statements.html

2. 第二部分是关于数据如何在TinyKV上的表示。

    单纯从完成这个作业的角度来看，也没啥好说的，无非是理解了数据结构的编码原理之后，按部就班的写代码。这里有个思考题：如果从Join的角度考虑，数据应该如何映射？尝试分析下。

    Join必然涉及到多表之间的关联。Join最耗时的地方就在于按列查找对应的记录。如果还将列放到value中，那么就要全部读一遍然后检查是否匹配，所以如果列能直接编码到key上，那么查找起来应该会更快一些，因为可以直接构造一个对应的key，然后查找是否存在即可。

    当完成作业后，这样测试：

    ➜ tablecodec git:(feature/proj1) ✗ go test -check.f testTableCodecSuite
    OK: 12 passed
    PASS
    ok github.com/pingcap/tidb/tablecodec 0.010s

    ➜ tablecodec git:(feature/proj1) ✗ go test -bench=.
    OK: 12 passed
    goos: linux
    goarch: amd64
    pkg: github.com/pingcap/tidb/tablecodec
    BenchmarkEncodeRowKeyWithHandle-12 31673216 36.7 ns/op
    BenchmarkEncodeEndKey-12 17469206 75.5 ns/op
    BenchmarkEncodeRowKeyWithPrefixNex-12 20127945 63.4 ns/op
    BenchmarkDecodeRowKey-12 326636646 3.66 ns/op
    BenchmarkHasTablePrefix-12 1000000000 0.323 ns/op
    BenchmarkHasTablePrefixBuiltin-12 210890934 5.69 ns/op
    BenchmarkEncodeValue-12 7399618 163 ns/op
    PASS
    ok github.com/pingcap/tidb/tablecodec 9.029s

