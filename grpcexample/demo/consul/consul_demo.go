package main

import (
	"fmt"
	consulApi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/api/watch"
)

// 使用consul源码中的watch包监听服务变化
func main() {
	var (
		err    error
		params map[string]interface{}
		plan   *watch.Plan
		ch     chan int
	)
	ch = make(chan int, 1)

	params = make(map[string]interface{})
	params["type"] = "service"
	params["service"] = "test"
	params["passingonly"] = false
	params["tag"] = "SERVER"
	//根据param参数，获取对应的plan
	plan, err = watch.Parse(params)
	if err != nil {
		panic(err)
	}
	//为plan指定自定义handle
	plan.Handler = func(index uint64, result interface{}) {
		if entries, ok := result.([]*consulApi.ServiceEntry); ok {
			fmt.Printf("serviceEntries:%v\n", entries)
			for i, entry := range entries {
				fmt.Printf("%d: %+v\n", i, entry)
				fmt.Printf("***Service: %+v\n", entry.Service)
				fmt.Printf("***Node: %+v\n", entry.Node)
				fmt.Printf("***Checks: %+v\n", entry.Checks)
			}
			// your code
			ch <- 1
		}
	}
	go func() {
		// your consul agent addr
		if err = plan.Run("127.0.0.1:8500"); err != nil {
			panic(err)
		}
	}()
	go register()
	for {
		<-ch
		fmt.Printf("get change")
	}
}

func register() {
	var (
		err    error
		client *consulApi.Client
	)
	client, err = consulApi.NewClient(&consulApi.Config{Address: "127.0.0.1:8500"})
	if err != nil {
		panic(err)
	}
	err = client.Agent().ServiceRegister(&consulApi.AgentServiceRegistration{
		ID:   "",
		Name: "test",
		Tags: []string{"SERVER"},
		Port: 8080,
		Check: &consulApi.AgentServiceCheck{
			HTTP: "",
		},
	})
	if err != nil {
		panic(err)
	}
}
