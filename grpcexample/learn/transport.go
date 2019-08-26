package learn

//go micro中transport解析
//https://www.jianshu.com/p/fc3630d741d4
//https://blog.csdn.net/u013272009/article/details/94772751
/*
//transport模块目录介绍
.
├── grpc                         // transport grpc 插件
│   ├── grpc.go                  // transport 接口实现代码
│   ├── grpc_test.go             // 测试用例
│   ├── handler.go
│   ├── proto                    // transport.proto 定义了`transport grpc 插件`网络层内部通信协议
│   │   ├── transport.micro.go   // 自动生成
│   │   ├── transport.pb.go      // 自动生成
│   │   └── transport.proto      // 网络层内部通信协议
│   └── socket.go                // socket 接口、 client 接口以及 listener 接口实现
├── http                         // transport http 插件
│   ├── http.go                  // transport 接口实现代码。这里只是包裹下，真正实现在外面 http_*.go
│   ├── http_test.go             // 测试用例
│   └── options.go               // 提供额外的 http 服务接口（福利）
├── http_proxy.go                // transport http 插件，建立连接细节
├── http_transport.go            // transport http 插件，transport 接口、 socket 接口、 client 接口以及 listener 接口实现
├── http_transport_test.go       // 测试用例
├── memory                       // transport memory 插件
│   ├── memory.go                // transport memory 插件，transport 接口、 socket 接口、 client 接口以及 listener 接口实现
│   └── memory_test.go           // 测试用例
├── options.go                   // transport http 插件，相关 option
├── quic                         // transport quic 插件
│   └── quic.go                  // transport quic 插件，transport 接口、 socket 接口、 client 接口以及 listener 接口实现
└── transport.go                 // transport 模块接口定义
 */
//官方提供的transport模块使用例子

import (
	"testing"
	"github.com/micro/go-micro/transport"
)

func TestMemoryTransport(t *testing.T) {
	tr := transport.NewTransport()

	// bind / listen
	l, err := tr.Listen("127.0.0.1:8080")
	if err != nil {
		t.Fatalf("Unexpected error listening %v", err)
	}
	defer l.Close()

	// accept
	go func() {
		if err := l.Accept(func(sock transport.Socket) {
			for {
				var m transport.Message
				if err := sock.Recv(&m); err != nil {
					return
				}
				t.Logf("Server Received %s", string(m.Body))
				if err := sock.Send(&transport.Message{
					Body: []byte(`pong`),
				}); err != nil {
					return
				}
			}
		}); err != nil {
			t.Fatalf("Unexpected error accepting %v", err)
		}
	}()

	// dial
	c, err := tr.Dial("127.0.0.1:8080")
	if err != nil {
		t.Fatalf("Unexpected error dialing %v", err)
	}
	defer c.Close()

	// send <=> receive
	for i := 0; i < 3; i++ {
		if err := c.Send(&transport.Message{
			Body: []byte(`ping`),
		}); err != nil {
			return
		}
		var m transport.Message
		if err := c.Recv(&m); err != nil {
			return
		}
		t.Logf("Client Received %s", string(m.Body))
	}

}

func TestListener(t *testing.T) {
	tr := transport.NewTransport()

	// bind / listen on random port
	l, err := tr.Listen(":0")
	if err != nil {
		t.Fatalf("Unexpected error listening %v", err)
	}
	defer l.Close()

	// try again
	l2, err := tr.Listen(":0")
	if err != nil {
		t.Fatalf("Unexpected error listening %v", err)
	}
	defer l2.Close()

	// now make sure it still fails
	l3, err := tr.Listen(":8080")
	if err != nil {
		t.Fatalf("Unexpected error listening %v", err)
	}
	defer l3.Close()

	if _, err := tr.Listen(":8080"); err == nil {
		t.Fatal("Expected error binding to :8080 got nil")
	}
}