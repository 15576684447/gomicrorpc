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

