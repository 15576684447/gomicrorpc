package learn

//检查rpc_router中注册的回调函数的合法性
//分为Stream方法和普通方法，分别检查其输入参数与输出参数是否符合规范
/*
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
 */
