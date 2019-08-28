package server

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/micro/go-micro/registry"
)
//该函数的功能为解析v的变量类型，其返回结果可能是一个多层结果
//如果为指针，则返回指针类型，接着解析指针类型(指针类型可能为struct等复合类型)
//如果是结构体，解析结构体成员，成员中可能还有更深的类型(如struct成员或者切片等构造成员)
//如果是切片类型，解析切片类型及其更深层次的类型(切片也有可能是结构体切片)
//直到解析到最基本的类型或者解析到第3层
func extractValue(v reflect.Type, d int) *registry.Value {
	if d == 3 {
		return nil
	}
	if v == nil {
		return nil
	}
	//如果是指针，返回指针类型
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if len(v.Name()) == 0 {
		return nil
	}

	arg := &registry.Value{
		Name: v.Name(),
		Type: v.Name(),
	}

	switch v.Kind() {
	//如果是struct结构体，遍历结构体每一个成员，并解析成员类型，最多解析三层
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			val := extractValue(f.Type, d+1)
			if val == nil {
				continue
			}

			// if we can find a json tag use it
			//如果变量tag为json
			if tags := f.Tag.Get("json"); len(tags) > 0 {
				parts := strings.Split(tags, ",")
				//如果变量定义为可忽略，则忽略，继续
				if parts[0] == "-" || parts[0] == "omitempty" {
					continue
				}
				//否则，不可忽略
				val.Name = parts[0]
			}

			// if there's no name default it
			//如果没有在描述字段描述name，则获取该字段名作为变量名
			if len(val.Name) == 0 {
				val.Name = v.Field(i).Name
			}

			// still no name then continue
			if len(val.Name) == 0 {
				continue
			}

			arg.Values = append(arg.Values, val)
		}
		//如果是切片类型
	case reflect.Slice:
		//获取元素类型
		p := v.Elem()
		//如果元素类型为指针，直接获取指针元素类型
		if p.Kind() == reflect.Ptr {
			p = p.Elem()
		}
		arg.Type = "[]" + p.Name()
		//继续获取下一级成员类型，最多解析3层或者解析到nil为止
		val := extractValue(v.Elem(), d+1)
		if val != nil {
			arg.Values = append(arg.Values, val)
		}
	}

	return arg
}
//输入一个方法，获取方法名，req具体参数类型，rsp具体参数类型等信息
func extractEndpoint(method reflect.Method) *registry.Endpoint {
	if method.PkgPath != "" {
		return nil
	}

	var rspType, reqType reflect.Type
	var stream bool
	mt := method.Type

	switch mt.NumIn() {
	case 3:
		reqType = mt.In(1)
		rspType = mt.In(2)
	case 4:
		reqType = mt.In(2)
		rspType = mt.In(3)
	default:
		return nil
	}

	// are we dealing with a stream?
	switch rspType.Kind() {
	case reflect.Func, reflect.Interface:
		stream = true
	}
	//解析req与rsp参数类型，解析到最基本的类型为止，或者解析到第3层(如req类型为struct，则解析struct成员至最基本的类型为止)
	request := extractValue(reqType, 0)
	response := extractValue(rspType, 0)

	return &registry.Endpoint{
		Name:     method.Name,
		Request:  request,
		Response: response,
		Metadata: map[string]string{
			"stream": fmt.Sprintf("%v", stream),
		},
	}
}

func extractSubValue(typ reflect.Type) *registry.Value {
	var reqType reflect.Type
	switch typ.NumIn() {
	case 1:
		reqType = typ.In(0)
	case 2:
		reqType = typ.In(1)
	case 3:
		reqType = typ.In(2)
	default:
		return nil
	}
	return extractValue(reqType, 0)
}
