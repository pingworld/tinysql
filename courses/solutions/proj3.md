## 题解

### 心路

初看这部分似乎并无思路，所以我先看了这篇[文章](https://pingcap.com/blog-cn/tidb-source-code-reading-17/)。

沿着这篇文章的参考，我又找到了，这篇关于[F1论文中scheme变更的原理介绍](https://github.com/zimulala/builddatabase/blob/master/f1/schema-change.md)，及其相关[实现分析](http://zimulala.github.io/2016/02/02/schema-change-implement/)还有[这里](http://zimulala.github.io/2017/12/24/optimize/)。

看完这些内容，似乎也并没有透彻，所以我准备用自己的话简单的说下DDL变更的原理。

### 简单原理自述

既然是异步的，所以这里是一个Job添加到某个执行队列并排队执行的过程，参考代码：

```golang
// 启动DDL Job
func(d*ddl)doDDLJob(ctxsessionctx.Context,job*model.Job)error{


// DDL Job入队
func(d*ddl)addDDLJob(ctxsessionctx.Context,job*model.Job)error{


// 从队列中取出Job并处理
func(w*worker)handleDDLJobQueue(d*ddlCtx)error{

// Job完成后的通知
func(w*worker)finishDDLJob(t*meta.Meta,job*model.Job)(errerror){
```

以DropColumn(`只要修改 table 的元信息，把 table 元信息中对应的要删除的 column 删除`)为例，其基本流程是这样的：

```golang

ddl.DropColumn -> ddl.doDDLJob -> ddl.addDDLJob -> meta.EnQueueDDLJob

worker.handleDDLJobQueue -> worker.getFirstDDLJob -> worker.runDDLJob -> onDropColumn

```

参考其他DDL的执行，比如onAddColumn，你会发现它是一个类似状态机的实现，第一眼看有点懵，所以看这个描述：https://github.com/zimulala/builddatabase/blob/master/f1/schema-change.md#f1-%E4%B8%AD%E7%9A%84%E7%AE%97%E6%B3%95%E5%AE%9E%E7%8E%B0，然后加下日志，基本上就能知道个大概了。

Drop Column分为这么几个阶段：
model.StatePublic（初始状态） -> model.StateWriteOnly（指的是 schema 元素对写操作可见，对读操作不可见） -> model.StateDeleteOnly（指的是 schema 元素的存在性只对删除操作可见） -> model.StateDeleteReorganization（保证在索引变为 public 之前所有旧数据的索引都被正确地生成） -> model.StateNone（完成）

参考：
https://github.com/ngaut/builddatabase
http://disksing.com/understanding-f1-schema-change/