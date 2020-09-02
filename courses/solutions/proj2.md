## 题解

本章主要是掌握SQL的语法解析器。

对于语法解析，过往只有感性的认识，就是知道它是干啥的——将文本格式的语法解析成语法树，以供后续分析。也知道这块东西属于编译领域。但由于离平常的编码工作挺远的，除了大学时写过简单的LR词法分析之外，几乎从未深究过。所以，这块内容我需要补充一些先验知识。下面是我为了完成本章题目而走过的路：

1. TinySQL是如何解析SQL的，大体的流程，这个可以参考TiDB源码阅读中的那一篇（ https://pingcap.com/blog-cn/tidb-source-code-reading-5/ ）文章。以我的经验，如果没有Fex&Bision的常识，这篇文章看起来如蜻蜓点水，并不能带来太深的认识。
   
2. 看完上面那篇官方提供的参考，你会自然的想了解Lex&Yacc，因为看过之后根本不知道为啥这样，所以需要了解甚至是深入认识Fex&Bision的常识，简单了解了这些常识之后，再回头看1中说的文章就比较轻松了。这里我参考的《Lex&Yacc 2nd》和《Flex&Bison》的前3章。
   
3. 在回头看1中的文章时，我发现阅读parser的Makefile文件对于整体的理解有一定的帮助，所以我在Makefile中加了一些注释。
   
4. 有了Flex常识之后，你发现还是没法完成作业，但大体明白是怎么回事了，至少看`parser/parser.y`这个文件不那么陌生了。读一读TiDB的parser.y文件，你会发现它跟MySQL的语法定义很相似，再看下MySQL的语法定义，熟悉下SQL的BNF（ https://github.com/ronsavage/SQL/blob/master/sql-2003-1.bnf ）定义，以及参考《Flex&Bison》的第4章，专门用来写SQL解析的。
   
5. 这样的学习，大约需要1到2周吧，反正我就用了差不多2周左右的时间，每天大约1、2个小时。这时候再看作业，你发现变得亲切多了，也知道改哪里，大体上怎么改了。多改几遍，你就会找到一些技巧。
   
6. 测试，也许还可以多加一些测试case，这个作业似乎没有要求实现所有Join的规则，反正它也说了通过Test就可以满分，哈哈

测试方法：
```sh
./bin/goyacc -o parser.go parser.y 2>&1
go test -check.f TestDMLStmt

或者

make all
go test -check.f TestDMLStmt
```
