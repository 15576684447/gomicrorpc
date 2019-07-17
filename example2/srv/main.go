package main

import (
	"github.com/micro/go-micro"
	"github.com/micro/go-micro/registry"
	"github.com/micro/go-micro/registry/consul"
	"github.com/micro/go-micro/server"
	"learn/gomicrorpc/example2/common"
	"learn/gomicrorpc/example2/handler"
	"learn/gomicrorpc/example2/proto/rpcapi"
	"learn/gomicrorpc/example2/subscriber"
	"time"
)

func main() {
	reg := consul.NewRegistry(func(op *registry.Options) { //使用consul作为服务发现
		op.Addrs = []string{
			"127.0.0.1:8500",
		}
	})
	// 初始化服务
	service := micro.NewService(
		micro.Registry(reg),
		micro.Name(common.ServiceName2),
		micro.RegisterTTL(time.Second*30),
		micro.RegisterInterval(time.Second*20))
	/*
		service := micro.NewService(//使用默认服务发现mdns
			micro.Name(common.ServiceName),
			micro.RegisterTTL(time.Second*30),
			micro.RegisterInterval(time.Second*20),
		)*/

	service.Init()
	// 注册 Handler
	rpcapi.RegisterSayHandler(service.Server(), new(handler.Say))

	// Register Subscribers
	if err := server.Subscribe(server.NewSubscriber(common.Topic1, subscriber.Handler)); err != nil {
		panic(err)
	}

	// run server
	if err := service.Run(); err != nil {
		panic(err)
	}
}
