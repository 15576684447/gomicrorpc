package learn

//一个subscriber的handler函数的反射过程解读
/*
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
*/

//检测handler类型及合法性
/*
// prepareMethod returns a methodType for the provided method or nil
// in case if the method was unsuitable.
func prepareMethod(method reflect.Method) *methodType {
	mtype := method.Type
	mname := method.Name
	var replyType, argType, contextType reflect.Type
	var stream bool

	// Method must be exported.
	if method.PkgPath != "" {
		return nil
	}

	switch mtype.NumIn() {
	case 3:
		// assuming streaming
		argType = mtype.In(2)
		contextType = mtype.In(1)
		stream = true
	case 4:
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
		streamType := reflect.TypeOf((*Stream)(nil)).Elem()
		if !argType.Implements(streamType) {
			log.Log(mname, "argument does not implement Stream interface:", argType)
			return nil
		}
	} else {
		// if not stream check the replyType

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

	// Method needs one out.
	if mtype.NumOut() != 1 {
		log.Log("method", mname, "has wrong number of outs:", mtype.NumOut())
		return nil
	}
	// The return type of the method must be error.
	if returnType := mtype.Out(0); returnType != typeOfError {
		log.Log("method", mname, "returns", returnType.String(), "not error")
		return nil
	}
	return &methodType{method: method, ArgType: argType, ReplyType: replyType, ContextType: contextType, stream: stream}
}
*/
