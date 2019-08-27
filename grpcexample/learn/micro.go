package learn

//go micro系列
/*
http://btfak.com/%E5%BE%AE%E6%9C%8D%E5%8A%A1/2016/03/28/go-micro/
http://btfak.com/%E5%BE%AE%E6%9C%8D%E5%8A%A1/2016/04/11/micro-on-nats/
http://btfak.com/%E5%BE%AE%E6%9C%8D%E5%8A%A1/2016/05/15/resiliency/
*/

/*
Micro采用插件化的架构设计,用户可以替换底层的实现,而不更改任何底层的代码
每个Go-Micro框架的底层模块定义了相应的接口,registry是作为服务发现,transport作为同步通信,broker作为异步通信

Go Micro包括以下这些包和功能
	Registry：客户端的服务发现
	Transport：同步通信
	Broker：异步通信
	Selector：节点过滤、负载均衡
	Codec：消息编解码
	Server：基于此库构建RPC服务端
	Client：基于此库构建RPC客户端
*/
