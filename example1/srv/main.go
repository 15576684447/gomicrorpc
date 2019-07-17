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

type Say struct{}

func (s *Say) Hello(ctx context.Context, req *model.SayParam, rsp *model.SayResponse) error {
	fmt.Println("received", req.Msg)
	rsp.Header = make(map[string]*model.Pair)
	rsp.Header["name"] = &model.Pair{Key: 1, Values: "abc"}

	rsp.Msg = "hello world"
	rsp.Values = append(rsp.Values, "a", "b")
	rsp.Type = model.RespType_DESCEND

	return nil
}

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
			micro.Name("lp.srv.eg1"),
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
		service := micro.NewService(//默认使用mdns作为服务发现
			micro.Name("lp.srv.eg1"),
		)
	*/

	service.Init()

	// 注册 Handler
	model.RegisterSayHandler(service.Server(), new(Say))

	// run server
	if err := service.Run(); err != nil {
		panic(err)
	}
}
