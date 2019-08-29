package net

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Listen takes addr:portmin-portmax and binds to the first available port
// Example: Listen("localhost:5000-6000", fn)
//addr为输入IP:[port...]，指定一个port区间，是为了尝试绑定区间内可行的 IP:host，有成功就返回，否则返回失败(未找到)
//如果port只指定了一个，直接返回fn
func Listen(addr string, fn func(string) (net.Listener, error)) (net.Listener, error) {
	// host:port || host:min-max
	parts := strings.Split(addr, ":")

	//如果只有IP，返回fn
	if len(parts) < 2 {
		return fn(addr)
	}

	// try to extract port range
	ports := strings.Split(parts[len(parts)-1], "-")

	// single port
	//如果只有单个port，返回fn
	if len(ports) < 2 {
		return fn(addr)
	}

	// we have a port range

	// extract min port
	//只有指定了一个范围的port，才使用go-mocro-net函数，解析最小port值和最大port值
	min, err := strconv.Atoi(ports[0])
	if err != nil {
		return nil, errors.New("unable to extract port range")
	}

	// extract max port
	max, err := strconv.Atoi(ports[1])
	if err != nil {
		return nil, errors.New("unable to extract port range")
	}

	// set host
	//获取主机地址
	host := parts[:len(parts)-1]

	// range the ports
	//遍历port，尝试使用所有port绑定，一旦绑定成功，就返回，否则返回错误
	for port := min; port <= max; port++ {
		// try bind to host:port
		ln, err := fn(fmt.Sprintf("%s:%d", host, port))
		//只要其中一个绑定成功就返回
		if err == nil {
			return ln, nil
		}

		// hit max port
		//如果所有port都没绑定成功，返回错误
		if port == max {
			return nil, err
		}
	}

	// why are we here?
	return nil, fmt.Errorf("unable to bind to %s", addr)
}
