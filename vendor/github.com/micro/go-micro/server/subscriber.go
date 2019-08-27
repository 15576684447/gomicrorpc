package server

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/micro/go-micro/broker"
	"github.com/micro/go-micro/codec"
	"github.com/micro/go-micro/metadata"
	"github.com/micro/go-micro/registry"
)

const (
	subSig = "func(context.Context, interface{}) error"
)

type handler struct {
	method  reflect.Value
	reqType reflect.Type
	ctxType reflect.Type
}

type subscriber struct {
	topic      string
	rcvr       reflect.Value
	typ        reflect.Type
	subscriber interface{}
	handlers   []*handler
	endpoints  []*registry.Endpoint
	opts       SubscriberOptions
}

func newSubscriber(topic string, sub interface{}, opts ...SubscriberOption) Subscriber {
	options := SubscriberOptions{
		AutoAck: true,
	}

	for _, o := range opts {
		o(&options)
	}

	var endpoints []*registry.Endpoint
	var handlers []*handler

	if typ := reflect.TypeOf(sub); typ.Kind() == reflect.Func {
		h := &handler{
			method: reflect.ValueOf(sub),
		}

		switch typ.NumIn() {
		case 1:
			h.reqType = typ.In(0)
		case 2:
			h.ctxType = typ.In(0)
			h.reqType = typ.In(1)
		}

		handlers = append(handlers, h)

		endpoints = append(endpoints, &registry.Endpoint{
			Name:    "Func",
			Request: extractSubValue(typ),
			Metadata: map[string]string{
				"topic":      topic,
				"subscriber": "true",
			},
		})
	} else {
		hdlr := reflect.ValueOf(sub)
		name := reflect.Indirect(hdlr).Type().Name()

		for m := 0; m < typ.NumMethod(); m++ {
			method := typ.Method(m)
			h := &handler{
				method: method.Func,
			}

			switch method.Type.NumIn() {
			case 2:
				h.reqType = method.Type.In(1)
			case 3:
				h.ctxType = method.Type.In(1)
				h.reqType = method.Type.In(2)
			}

			handlers = append(handlers, h)

			endpoints = append(endpoints, &registry.Endpoint{
				Name:    name + "." + method.Name,
				Request: extractSubValue(method.Type),
				Metadata: map[string]string{
					"topic":      topic,
					"subscriber": "true",
				},
			})
		}
	}

	return &subscriber{
		rcvr:       reflect.ValueOf(sub),
		typ:        reflect.TypeOf(sub),
		topic:      topic,
		subscriber: sub,
		handlers:   handlers,
		endpoints:  endpoints,
		opts:       options,
	}
}

func validateSubscriber(sub Subscriber) error {
	//反射获取类型
	typ := reflect.TypeOf(sub.Subscriber())
	var argType reflect.Type
	//如果类型为函数
	if typ.Kind() == reflect.Func {
		name := "Func"
		//获取该函数传入的参数个数
		switch typ.NumIn() {
		case 2://如果两个参数，获取第2个[参数序号从0开始]
			argType = typ.In(1)
		default:
			return fmt.Errorf("subscriber %v takes wrong number of args: %v required signature %s", name, typ.NumIn(), subSig)
		}
		if !isExportedOrBuiltinType(argType) {
			return fmt.Errorf("subscriber %v argument type not exported: %v", name, argType)
		}
		//检测函数返回参数个数是否为1个
		if typ.NumOut() != 1 {
			return fmt.Errorf("subscriber %v has wrong number of outs: %v require signature %s",
				name, typ.NumOut(), subSig)
		}
		//如果返回参数个数为1个，则获取第1个参数
		if returnType := typ.Out(0); returnType != typeOfError {
			return fmt.Errorf("subscriber %v returns %v not error", name, returnType.String())
		}
	} else {//如果反射类型不是函数，则可能是对象类型
		//获取对象抽象值
		hdlr := reflect.ValueOf(sub.Subscriber())
		//获取对象抽象值指向值的类型名，即结构体名
		name := reflect.Indirect(hdlr).Type().Name()
		//获取该对象包含的所有方法，并遍历
		for m := 0; m < typ.NumMethod(); m++ {
			//获取每一个具体方法
			method := typ.Method(m)
			//获取该方法传入的参数个数
			switch method.Type.NumIn() {
			case 3://获取传入参数为3个，获取最后一个
				argType = method.Type.In(2)
			default:
				return fmt.Errorf("subscriber %v.%v takes wrong number of args: %v required signature %s",
					name, method.Name, method.Type.NumIn(), subSig)
			}

			if !isExportedOrBuiltinType(argType) {
				return fmt.Errorf("%v argument type not exported: %v", name, argType)
			}
			//检查返回的参数个数是否为1个
			if method.Type.NumOut() != 1 {
				return fmt.Errorf(
					"subscriber %v.%v has wrong number of outs: %v require signature %s",
					name, method.Name, method.Type.NumOut(), subSig)
			}
			//如果返回参数个数为1个，则获取第1个参数
			if returnType := method.Type.Out(0); returnType != typeOfError {
				return fmt.Errorf("subscriber %v.%v returns %v not error", name, method.Name, returnType.String())
			}
		}
	}

	return nil
}

func (s *rpcServer) createSubHandler(sb *subscriber, opts Options) broker.Handler {
	return func(p broker.Event) error {
		msg := p.Message()

		// get codec
		ct := msg.Header["Content-Type"]

		// default content type
		if len(ct) == 0 {
			msg.Header["Content-Type"] = DefaultContentType
			ct = DefaultContentType
		}

		// get codec
		cf, err := s.newCodec(ct)
		if err != nil {
			return err
		}

		// copy headers
		hdr := make(map[string]string)
		for k, v := range msg.Header {
			hdr[k] = v
		}

		// create context
		ctx := metadata.NewContext(context.Background(), hdr)

		results := make(chan error, len(sb.handlers))

		for i := 0; i < len(sb.handlers); i++ {
			handler := sb.handlers[i]

			var isVal bool
			var req reflect.Value

			if handler.reqType.Kind() == reflect.Ptr {
				req = reflect.New(handler.reqType.Elem())
			} else {
				req = reflect.New(handler.reqType)
				isVal = true
			}
			if isVal {
				req = req.Elem()
			}

			b := &buffer{bytes.NewBuffer(msg.Body)}
			co := cf(b)
			defer co.Close()

			if err := co.ReadHeader(&codec.Message{}, codec.Event); err != nil {
				return err
			}

			if err := co.ReadBody(req.Interface()); err != nil {
				return err
			}

			fn := func(ctx context.Context, msg Message) error {
				var vals []reflect.Value
				if sb.typ.Kind() != reflect.Func {
					vals = append(vals, sb.rcvr)
				}
				if handler.ctxType != nil {
					vals = append(vals, reflect.ValueOf(ctx))
				}

				vals = append(vals, reflect.ValueOf(msg.Payload()))

				returnValues := handler.method.Call(vals)
				if err := returnValues[0].Interface(); err != nil {
					return err.(error)
				}
				return nil
			}

			for i := len(opts.SubWrappers); i > 0; i-- {
				fn = opts.SubWrappers[i-1](fn)
			}

			if s.wg != nil {
				s.wg.Add(1)
			}

			go func() {
				if s.wg != nil {
					defer s.wg.Done()
				}

				results <- fn(ctx, &rpcMessage{
					topic:       sb.topic,
					contentType: ct,
					payload:     req.Interface(),
				})
			}()
		}

		var errors []string

		for i := 0; i < len(sb.handlers); i++ {
			if err := <-results; err != nil {
				errors = append(errors, err.Error())
			}
		}

		if len(errors) > 0 {
			return fmt.Errorf("subscriber error: %s", strings.Join(errors, "\n"))
		}

		return nil
	}
}

func (s *subscriber) Topic() string {
	return s.topic
}

func (s *subscriber) Subscriber() interface{} {
	return s.subscriber
}

func (s *subscriber) Endpoints() []*registry.Endpoint {
	return s.endpoints
}

func (s *subscriber) Options() SubscriberOptions {
	return s.opts
}
