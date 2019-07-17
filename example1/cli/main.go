package main

import (
	"context"
	"fmt"
	"github.com/micro/go-micro"
	"github.com/micro/go-micro/registry"
	"github.com/micro/go-micro/registry/consul"
	model "learn/gomicrorpc/example1/proto"
	"learn/gomicrorpc/example2/common"
)

func main() {
	// 我这里用的etcd 做为服务发现
	/*
			reg := etcdv3.NewRegistry(func(op *registry.Options) {
				op.Addrs = []string{
					"http://192.168.3.34:2379", "http://192.168.3.18:2379", "http://192.168.3.110:2379",
				}
			})


		// 初始化服务
		service := micro.NewService(
			micro.Registry(reg),
		)
	*/
	//使用consul作为服务发现
	reg := consul.NewRegistry(func(op *registry.Options) { //使用consul作为服务发现
		op.Addrs = []string{
			"127.0.0.1:8500",
		}
	})
	// 初始化服务
	service := micro.NewService(micro.Registry(reg), micro.Name(common.ServiceName1))

	// 2019年源码有变动默认使用的是mdns面不是consul了
	// 如果你用的是默认的注册方式把上面的注释掉用下面的

	// 初始化服务
	/*
		service := micro.NewService(//使用默认服务发现mdns
			micro.Name(common.ServiceName1)),
		)
	*/

	service.Init()

	sayClent := model.NewSayService(common.ServiceName1, service.Client())

	rsp, err := sayClent.Hello(context.Background(), &model.SayParam{Msg: "hello server"})
	if err != nil {
		panic(err)
	}

	fmt.Println(rsp)

}
