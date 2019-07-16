package main

import (
	"github.com/micro/go-micro"
	"github.com/micro/go-micro/server"
	"learn/gomicrorpc/example2/common"
	"learn/gomicrorpc/example2/handler"
	"learn/gomicrorpc/example2/proto/rpcapi"
	"learn/gomicrorpc/example2/subscriber"
	"time"
)

func main() {
	// 初始化服务
	service := micro.NewService(
		micro.Name(common.ServiceName),
		micro.RegisterTTL(time.Second*30),
		micro.RegisterInterval(time.Second*20),
	)

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
