package server

import (
	"reflect"

	"github.com/micro/go-micro/registry"
)

type rpcHandler struct {
	name      string
	handler   interface{}
	endpoints []*registry.Endpoint
	opts      HandlerOptions
}
//将每一个对象解析为一个终端(endpoints)，其中的方法为子终端，(对象名.方法名)为子终端名称，参数类型为子终端参数类型
func newRpcHandler(handler interface{}, opts ...HandlerOption) Handler {
	options := HandlerOptions{
		Metadata: make(map[string]map[string]string),
	}

	for _, o := range opts {
		o(&options)
	}

	typ := reflect.TypeOf(handler)
	hdlr := reflect.ValueOf(handler)
	//获取对象名
	name := reflect.Indirect(hdlr).Type().Name()

	var endpoints []*registry.Endpoint
	//遍历该对象方法
	for m := 0; m < typ.NumMethod(); m++ {
		//获取该方法方法名，输入参数与返回参数(递归直到基本类型或者3层为止)
		if e := extractEndpoint(typ.Method(m)); e != nil {
			//方法名具体由对象名.方法名组合
			e.Name = name + "." + e.Name

			for k, v := range options.Metadata[e.Name] {
				e.Metadata[k] = v
			}
			//注册终端，对象的每个方法为一个子终端，包含子终端名(对象名.方法名)，子终端参数(输入参数与输出参数)
			endpoints = append(endpoints, e)
		}
	}
	//返回一个rpc注册表
	return &rpcHandler{
		name:      name,
		handler:   handler,
		endpoints: endpoints,
		opts:      options,
	}
}

func (r *rpcHandler) Name() string {
	return r.name
}

func (r *rpcHandler) Handler() interface{} {
	return r.handler
}

func (r *rpcHandler) Endpoints() []*registry.Endpoint {
	return r.endpoints
}

func (r *rpcHandler) Options() HandlerOptions {
	return r.opts
}
