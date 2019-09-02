// Package memory is an in-memory transport
package memory

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/micro/go-micro/transport"
)
//memory transport是一个本地连接，连接也是虚拟连接，memoryTransport中维护了一个map，用于记录服务端监听了哪些addr
//则对应的客户端在尝试连接时，都会去访问map，查看是否可连接，如果可以，则建立一个虚拟连接
//建立连接的步骤：
//Listen: 根据addr，在memoryTransport的[addr-memoryListener] map中查看是否已经有存在，如果有，说明已经存在监听；否则新建一个监听，对该memoryListener进行监听
//Dial: 根据addr，到memoryTransport的[addr-memoryListener] map中查看是否已经有存在，如果有，则说明有监听，可以连接，将memoryClient.memorySocket发送到对应memoryListener.memorySocket管道中
//Accept: 当对应的memoryListener.memorySocket的管道中收到memorySocket时，说明对应的Client请求连接，则将memorySocket参数取出，传递给handler函数(socket fd)
//Send: 将transport.Message发送到对应的memorySocket.send管道中
//Recv: 从memorySocket.recv管道中获取transport.Message

//Socket句柄结构体，用于正式的通信，包含发送管道和接收管道
type memorySocket struct {
	recv chan *transport.Message
	send chan *transport.Message
	// sock exit
	exit chan bool
	// listener exit
	lexit chan bool

	local  string
	remote string
	sync.RWMutex
}
//memory 客户端，包含初始化的memorySocket参数，连接时将memorySocket传递给对应memoryListener的memorySocket管道中，然后服务端接受，正式连接连接
type memoryClient struct {
	*memorySocket
	opts transport.DialOptions
}
//用于服务器监听，为客户端-服务器连接提供memorySocket管道，以传递memorySocket参数
type memoryListener struct {
	addr string
	exit chan bool
	conn chan *memorySocket
	opts transport.ListenOptions
	sync.RWMutex
}
//memory 结构体，包含一个[addr-memoryListener]map，为客户端-服务器连接提供memorySocket管道
type memoryTransport struct {
	opts transport.Options
	sync.RWMutex
	listeners map[string]*memoryListener
}
//memory 客户端方法，维护同一个memorySocket
//从memorySocket.recv接受消息
func (ms *memorySocket) Recv(m *transport.Message) error {
	ms.RLock()
	defer ms.RUnlock()
	select {
	case <-ms.exit:
		return errors.New("connection closed")
	case <-ms.lexit:
		return errors.New("server connection closed")
	case cm := <-ms.recv:
		*m = *cm
	}
	return nil
}

func (ms *memorySocket) Local() string {
	return ms.local
}

func (ms *memorySocket) Remote() string {
	return ms.remote
}
//将信息发送到memorySocket.send管道
func (ms *memorySocket) Send(m *transport.Message) error {
	ms.RLock()
	defer ms.RUnlock()
	select {
	case <-ms.exit:
		return errors.New("connection closed")
	case <-ms.lexit:
		return errors.New("server connection closed")
	case ms.send <- m:
	}
	return nil
}

func (ms *memorySocket) Close() error {
	ms.Lock()
	defer ms.Unlock()
	select {
	case <-ms.exit:
		return nil
	default:
		close(ms.exit)
	}
	return nil
}
//memory 服务端监听方法，accept后即返回一个客户端 memorySocket
func (m *memoryListener) Addr() string {
	return m.addr
}

func (m *memoryListener) Close() error {
	m.Lock()
	defer m.Unlock()
	select {
	case <-m.exit:
		return nil
	default:
		close(m.exit)
	}
	return nil
}

func (m *memoryListener) Accept(fn func(transport.Socket)) error {
	for {
		select {
		case <-m.exit:
			return nil
			//将客户端通过管道传递过来的memorySocket参数赋值到本地，如此客户端与服务器就拥有同一个memorySocket，并进行通信了
		case c := <-m.conn:
			go fn(&memorySocket{
				lexit:  c.lexit,
				exit:   c.exit,
				send:   c.recv,
				recv:   c.send,
				local:  c.Remote(),
				remote: c.Local(),
			})
		}
	}
}
//memory为本地解决方案，不可跨主机访问，客户端与服务器共用一个listeners map，用于存在监听的服务器地址addr，客户端dial之前需要查看服务端是否有监听对应的addr
func (m *memoryTransport) Dial(addr string, opts ...transport.DialOption) (transport.Client, error) {
	m.RLock()
	defer m.RUnlock()
	//查看dial的地址在服务端 map 中是否存在
	listener, ok := m.listeners[addr]
	if !ok {
		return nil, errors.New("could not dial " + addr)
	}

	var options transport.DialOptions
	for _, o := range opts {
		o(&options)
	}

	client := &memoryClient{
		&memorySocket{
			send:   make(chan *transport.Message),
			recv:   make(chan *transport.Message),
			exit:   make(chan bool),
			lexit:  listener.exit,
			local:  addr,
			remote: addr,
		},
		options,
	}

	// pseudo connect
	//建立虚假连接，如果对应服务端已经退出了，提示连接失败；否则将client.memorySocket传递给对应的listener.conn管道中
	select {
	case <-listener.exit:
		return nil, errors.New("connection error")
		//将client.memorySocket参数传递到listener.conn的管道
	case listener.conn <- client.memorySocket:
	}

	return client, nil
}

func (m *memoryTransport) Listen(addr string, opts ...transport.ListenOption) (transport.Listener, error) {
	m.Lock()
	defer m.Unlock()

	var options transport.ListenOptions
	for _, o := range opts {
		o(&options)
	}

	parts := strings.Split(addr, ":")

	// if zero port then randomly assign one
	//如果port为0，则随机分配一个
	if len(parts) > 1 && parts[len(parts)-1] == "0" {
		i := rand.Intn(20000)
		// set addr with port
		addr = fmt.Sprintf("%s:%d", parts[:len(parts)-1], 10000+i)
	}
	//查看是否已经存在listener
	if _, ok := m.listeners[addr]; ok {
		return nil, errors.New("already listening on " + addr)
	}

	listener := &memoryListener{
		opts: options,
		addr: addr,
		conn: make(chan *memorySocket),
		exit: make(chan bool),
	}
	//如果还不存在，则添加listener
	m.listeners[addr] = listener

	return listener, nil
}

func (m *memoryTransport) Init(opts ...transport.Option) error {
	for _, o := range opts {
		o(&m.opts)
	}
	return nil
}

func (m *memoryTransport) Options() transport.Options {
	return m.opts
}

func (m *memoryTransport) String() string {
	return "memory"
}
//memory初始化，新建一个listeners map，用于记录服务端监听了哪些addr
func NewTransport(opts ...transport.Option) transport.Transport {
	rand.Seed(time.Now().UnixNano())
	var options transport.Options
	for _, o := range opts {
		o(&options)
	}

	return &memoryTransport{
		opts:      options,
		listeners: make(map[string]*memoryListener),
	}
}
