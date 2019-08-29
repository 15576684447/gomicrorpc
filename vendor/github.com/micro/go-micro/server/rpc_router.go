package server

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Meh, we need to get rid of this shit

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/micro/go-micro/codec"
	"github.com/micro/go-micro/util/log"
)

var (
	lastStreamResponseError = errors.New("EOS")
	// A value sent as a placeholder for the server's response value when the server
	// receives an invalid request. It is never decoded by the client since the Response
	// contains an error when it is used.
	invalidRequest = struct{}{}

	// Precompute the reflect type for error. Can't use error directly
	// because Typeof takes an empty interface value. This is annoying.
	typeOfError = reflect.TypeOf((*error)(nil)).Elem()
)

type methodType struct {
	sync.Mutex  // protects counters
	method      reflect.Method
	ArgType     reflect.Type
	ReplyType   reflect.Type
	ContextType reflect.Type
	stream      bool
}

type service struct {
	name   string                 // name of service
	rcvr   reflect.Value          // receiver of methods for the service
	typ    reflect.Type           // type of the receiver
	method map[string]*methodType // registered methods
}

type request struct {
	msg  *codec.Message
	next *request // for free list in Server
}

type response struct {
	msg  *codec.Message
	next *response // for free list in Server
}

// router represents an RPC router.
type router struct {
	name         string
	mu           sync.Mutex // protects the serviceMap
	serviceMap   map[string]*service
	reqLock      sync.Mutex // protects freeReq
	freeReq      *request
	respLock     sync.Mutex // protects freeResp
	freeResp     *response
	hdlrWrappers []HandlerWrapper
}

func newRpcRouter() *router {
	return &router{
		serviceMap: make(map[string]*service),
	}
}

// Is this an exported - upper case - name?
func isExported(name string) bool {
	rune, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(rune)
}

// Is this type exported or a builtin?
func isExportedOrBuiltinType(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// PkgPath will be non-empty even for an exported type,
	// so we need to check the type name as well.
	return isExported(t.Name()) || t.PkgPath() == ""
}

// prepareMethod returns a methodType for the provided method or nil
// in case if the method was unsuitable.
func prepareMethod(method reflect.Method) *methodType {
	//获取方法类型
	mtype := method.Type
	//获取方法名
	mname := method.Name
	var replyType, argType, contextType reflect.Type
	var stream bool

	// Method must be exported.
	if method.PkgPath != "" {
		return nil
	}
	//获取方法输入参数个数
	switch mtype.NumIn() {
	case 3://共3个参数，判定为stream函数，获取参数类型和context类型
		// assuming streaming
		argType = mtype.In(2)
		contextType = mtype.In(1)
		stream = true
	case 4://4个参数，获取参数类型，返回类型以及context类型
		// method that takes a context
		argType = mtype.In(2)
		replyType = mtype.In(3)
		contextType = mtype.In(1)
	default:
		log.Log("method", mname, "of", mtype, "has wrong number of ins:", mtype.NumIn())
		return nil
	}

	if stream {
		// check stream type
		//如果参数类型为获Stream类型，获取Stream的特有接口，并检查argType是否实现了Stream的所有接口
		streamType := reflect.TypeOf((*Stream)(nil)).Elem()
		if !argType.Implements(streamType) {
			log.Log(mname, "argument does not implement Stream interface:", argType)
			return nil
		}
	} else {
		// if not stream check the replyType
		//如果参数类型不是Stream类型，则检查replyType是否为非指针，是否神明为export
		// First arg need not be a pointer.
		if !isExportedOrBuiltinType(argType) {
			log.Log(mname, "argument type not exported:", argType)
			return nil
		}

		if replyType.Kind() != reflect.Ptr {
			log.Log("method", mname, "reply type not a pointer:", replyType)
			return nil
		}

		// Reply type must be exported.
		if !isExportedOrBuiltinType(replyType) {
			log.Log("method", mname, "reply type not exported:", replyType)
			return nil
		}
	}
	//检查是否有一个参数返回
	// Method needs one out.
	if mtype.NumOut() != 1 {
		log.Log("method", mname, "has wrong number of outs:", mtype.NumOut())
		return nil
	}
	// The return type of the method must be error.
	//返回参数必须为error类型
	if returnType := mtype.Out(0); returnType != typeOfError {
		log.Log("method", mname, "returns", returnType.String(), "not error")
		return nil
	}
	return &methodType{method: method, ArgType: argType, ReplyType: replyType, ContextType: contextType, stream: stream}
}

func (router *router) sendResponse(sending sync.Locker, req *request, reply interface{}, cc codec.Writer, last bool) error {
	msg := new(codec.Message)
	msg.Type = codec.Response
	resp := router.getResponse()
	resp.msg = msg

	resp.msg.Id = req.msg.Id
	sending.Lock()
	err := cc.Write(resp.msg, reply)
	sending.Unlock()
	router.freeResponse(resp)
	return err
}

func (s *service) call(ctx context.Context, router *router, sending *sync.Mutex, mtype *methodType, req *request, argv, replyv reflect.Value, cc codec.Writer) error {
	defer router.freeRequest(req)
	//获取函数方法
	function := mtype.method.Func
	var returnValues []reflect.Value
	//解析req参数
	r := &rpcRequest{
		service:     req.msg.Target,
		contentType: req.msg.Header["Content-Type"],
		method:      req.msg.Method,
		endpoint:    req.msg.Endpoint,
		body:        req.msg.Body,
	}

	// only set if not nil
	if argv.IsValid() {
		r.rawBody = argv.Interface()
	}
	//如果不是Stream类型方法，执行如下
	if !mtype.stream {
		fn := func(ctx context.Context, req Request, rsp interface{}) error {
			//利用反射调用方法函数，并传入参数
			returnValues = function.Call([]reflect.Value{s.rcvr, mtype.prepareContext(ctx), reflect.ValueOf(argv.Interface()), reflect.ValueOf(rsp)})

			// The return value for the method is an error.
			//解析返回值是否为error类型，并且解析其结果
			if err := returnValues[0].Interface(); err != nil {
				return err.(error)
			}

			return nil
		}

		// wrap the handler
		for i := len(router.hdlrWrappers); i > 0; i-- {
			fn = router.hdlrWrappers[i-1](fn)
		}

		// execute handler
		//正式调用函数
		if err := fn(ctx, r, replyv.Interface()); err != nil {
			return err
		}

		// send response
		return router.sendResponse(sending, req, replyv.Interface(), cc, true)
	}
	//如果是Stream类型方法，执行如下
	// declare a local error to see if we errored out already
	// keep track of the type, to make sure we return
	// the same one consistently
	rawStream := &rpcStream{
		context: ctx,
		codec:   cc.(codec.Codec),
		request: r,
		id:      req.msg.Id,
	}

	// Invoke the method, providing a new value for the reply.
	fn := func(ctx context.Context, req Request, stream interface{}) error {
		//利用反射调用方法函数，并且传入参数
		returnValues = function.Call([]reflect.Value{s.rcvr, mtype.prepareContext(ctx), reflect.ValueOf(stream)})
		//检查返回值
		if err := returnValues[0].Interface(); err != nil {
			// the function returned an error, we use that
			return err.(error)
		} else if serr := rawStream.Error(); serr == io.EOF || serr == io.ErrUnexpectedEOF {
			return nil
		} else {
			// no error, we send the special EOS error
			return lastStreamResponseError
		}
	}

	// wrap the handler
	for i := len(router.hdlrWrappers); i > 0; i-- {
		fn = router.hdlrWrappers[i-1](fn)
	}

	// client.Stream request
	r.stream = true

	// execute handler
	//正式调用函数
	return fn(ctx, r, rawStream)
}

func (m *methodType) prepareContext(ctx context.Context) reflect.Value {
	if contextv := reflect.ValueOf(ctx); contextv.IsValid() {
		return contextv
	}
	return reflect.Zero(m.ContextType)
}

func (router *router) getRequest() *request {
	router.reqLock.Lock()
	req := router.freeReq
	if req == nil {
		req = new(request)
	} else {
		router.freeReq = req.next
		*req = request{}
	}
	router.reqLock.Unlock()
	return req
}

func (router *router) freeRequest(req *request) {
	router.reqLock.Lock()
	req.next = router.freeReq
	router.freeReq = req
	router.reqLock.Unlock()
}

func (router *router) getResponse() *response {
	router.respLock.Lock()
	resp := router.freeResp
	if resp == nil {
		resp = new(response)
	} else {
		router.freeResp = resp.next
		*resp = response{}
	}
	router.respLock.Unlock()
	return resp
}

func (router *router) freeResponse(resp *response) {
	router.respLock.Lock()
	resp.next = router.freeResp
	router.freeResp = resp
	router.respLock.Unlock()
}

func (router *router) readRequest(r Request) (service *service, mtype *methodType, req *request, argv, replyv reflect.Value, keepReading bool, err error) {
	cc := r.Codec()

	service, mtype, req, keepReading, err = router.readHeader(cc)
	if err != nil {
		if !keepReading {
			return
		}
		// discard body
		cc.ReadBody(nil)
		return
	}
	// is it a streaming request? then we don't read the body
	if mtype.stream {
		cc.ReadBody(nil)
		return
	}

	// Decode the argument value.
	argIsValue := false // if true, need to indirect before calling.
	if mtype.ArgType.Kind() == reflect.Ptr {
		argv = reflect.New(mtype.ArgType.Elem())
	} else {
		argv = reflect.New(mtype.ArgType)
		argIsValue = true
	}
	// argv guaranteed to be a pointer now.
	if err = cc.ReadBody(argv.Interface()); err != nil {
		return
	}
	if argIsValue {
		argv = argv.Elem()
	}

	if !mtype.stream {
		replyv = reflect.New(mtype.ReplyType.Elem())
	}
	return
}

func (router *router) readHeader(cc codec.Reader) (service *service, mtype *methodType, req *request, keepReading bool, err error) {
	// Grab the request header.
	msg := new(codec.Message)
	msg.Type = codec.Request
	req = router.getRequest()
	req.msg = msg

	err = cc.ReadHeader(msg, msg.Type)
	if err != nil {
		req = nil
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return
		}
		err = errors.New("rpc: router cannot decode request: " + err.Error())
		return
	}

	// We read the header successfully. If we see an error now,
	// we can still recover and move on to the next request.
	keepReading = true

	serviceMethod := strings.Split(req.msg.Endpoint, ".")
	if len(serviceMethod) != 2 {
		err = errors.New("rpc: service/endpoint request ill-formed: " + req.msg.Endpoint)
		return
	}
	// Look up the request.
	router.mu.Lock()
	service = router.serviceMap[serviceMethod[0]]
	router.mu.Unlock()
	if service == nil {
		err = errors.New("rpc: can't find service " + serviceMethod[0])
		return
	}
	mtype = service.method[serviceMethod[1]]
	if mtype == nil {
		err = errors.New("rpc: can't find method " + serviceMethod[1])
	}
	return
}

func (router *router) NewHandler(h interface{}, opts ...HandlerOption) Handler {
	return newRpcHandler(h, opts...)
}

func (router *router) Handle(h Handler) error {
	router.mu.Lock()
	defer router.mu.Unlock()
	//如果还没serviceMap，新建一个
	if router.serviceMap == nil {
		router.serviceMap = make(map[string]*service)
	}

	if len(h.Name()) == 0 {
		return errors.New("rpc.Handle: handler has no name")
	}
	if !isExported(h.Name()) {
		return errors.New("rpc.Handle: type " + h.Name() + " is not exported")
	}

	rcvr := h.Handler()
	s := new(service)
	s.typ = reflect.TypeOf(rcvr)
	s.rcvr = reflect.ValueOf(rcvr)

	// check name
	//如果serviceMap已经注册了该服务，则不再注册
	if _, present := router.serviceMap[h.Name()]; present {
		return errors.New("rpc.Handle: service already defined: " + h.Name())
	}

	s.name = h.Name()
	//如果还未注册，则新建一个name-methodType的方法映射表，方法名为(对象名.方法名)组合，如Say.Hello
	s.method = make(map[string]*methodType)

	// Install the methods
	//遍历所有方法，安装该handler的所有方法
	for m := 0; m < s.typ.NumMethod(); m++ {
		//获取方法
		method := s.typ.Method(m)
		//获取对应方法的方法名，参数名等
		if mt := prepareMethod(method); mt != nil {
			//安装方法
			s.method[method.Name] = mt
		}
	}

	// Check there are methods
	if len(s.method) == 0 {
		return errors.New("rpc Register: type " + s.name + " has no exported methods of suitable type")
	}

	// save handler
	//将该handler的注册表加入到serviceMap的路由表中
	router.serviceMap[s.name] = s
	return nil
}
//服务端响应函数调用请求
func (router *router) ServeRequest(ctx context.Context, r Request, rsp Response) error {
	sending := new(sync.Mutex)
	//解析请求的参数
	service, mtype, req, argv, replyv, keepReading, err := router.readRequest(r)
	if err != nil {
		if !keepReading {
			return err
		}
		// send a response if we actually managed to read a header.
		if req != nil {
			router.freeRequest(req)
		}
		return err
	}
	//service发起真正的调用请求，通过反射实现
	return service.call(ctx, router, sending, mtype, req, argv, replyv, rsp.Codec())
}
