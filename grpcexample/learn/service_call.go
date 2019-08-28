package learn

/*go-micro的service端收到请求后，
decode各项参数，并且发起内部rpcRequest，
通过rpc_router找到对应映射的方法，
最后通过reflect调用相关方法，最终的Call函数如下
 */

/*
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
 */