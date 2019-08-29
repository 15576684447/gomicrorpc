// Package server is an interface for a micro server
package server

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"github.com/micro/go-micro/codec"
	"github.com/micro/go-micro/registry"
	log "github.com/micro/go-micro/util/log"
)

// Server is a simple micro server abstraction
type Server interface {
	Options() Options
	Init(...Option) error
	Handle(Handler) error//将rpcHandler注册表添加到serviceMap路由表中
	//NewHandler获取了registry.Endpoint信息，但是在Handle时并没有用到，为啥？
	NewHandler(interface{}, ...HandlerOption) Handler//返回一个该接口的rpcHandler注册表
	NewSubscriber(string, interface{}, ...SubscriberOption) Subscriber//根据传入的接口(可能是函数，也可能是对象),解析方法名以及参数列表，生成handler表以及endpoint表
	Subscribe(Subscriber) error//检查subscribe接口的合法性，如果合法并且接口未注册，则注册至subscribers表格
	Start() error
	Stop() error
	String() string
}

// Router handle serving messages
type Router interface {
	// ServeRequest processes a request to completion
	ServeRequest(context.Context, Request, Response) error
}

// Message is an async message interface
type Message interface {
	Topic() string
	Payload() interface{}
	ContentType() string
}

// Request is a synchronous request interface
type Request interface {
	// Service name requested
	Service() string
	// The action requested
	Method() string
	// Endpoint name requested
	Endpoint() string
	// Content type provided
	ContentType() string
	// Header of the request
	Header() map[string]string
	// Body is the initial decoded value
	Body() interface{}
	// Read the undecoded request body
	Read() ([]byte, error)
	// The encoded message stream
	Codec() codec.Reader
	// Indicates whether its a stream
	Stream() bool
}

// Response is the response writer for unencoded messages
type Response interface {
	// Encoded writer
	Codec() codec.Writer
	// Write the header
	WriteHeader(map[string]string)
	// write a response directly to the client
	Write([]byte) error
}

// Stream represents a stream established with a client.
// A stream can be bidirectional which is indicated by the request.
// The last error will be left in Error().
// EOF indicates end of the stream.
type Stream interface {
	Context() context.Context
	Request() Request
	Send(interface{}) error
	Recv(interface{}) error
	Error() error
	Close() error
}

// Handler interface represents a request handler. It's generated
// by passing any type of public concrete object with endpoints into server.NewHandler.
// Most will pass in a struct.
//
// Example:
//
//      type Greeter struct {}
//
//      func (g *Greeter) Hello(context, request, response) error {
//              return nil
//      }
//
type Handler interface {
	Name() string
	Handler() interface{}
	Endpoints() []*registry.Endpoint
	Options() HandlerOptions
}

// Subscriber interface represents a subscription to a given topic using
// a specific subscriber function or object with endpoints.
type Subscriber interface {
	Topic() string
	Subscriber() interface{}
	Endpoints() []*registry.Endpoint
	Options() SubscriberOptions
}

type Option func(*Options)

var (
	DefaultAddress              = ":0"
	DefaultName                 = "server"
	DefaultVersion              = "latest"
	DefaultId                   = uuid.New().String()
	DefaultServer        Server = newRpcServer()
	DefaultRouter               = newRpcRouter()
	DefaultRegisterCheck        = func(context.Context) error { return nil }
)

// DefaultOptions returns config options for the default service
func DefaultOptions() Options {
	return DefaultServer.Options()
}

// Init initialises the default server with options passed in
func Init(opt ...Option) {
	if DefaultServer == nil {
		DefaultServer = newRpcServer(opt...)
	}
	DefaultServer.Init(opt...)
}

// NewServer returns a new server with options passed in
func NewServer(opt ...Option) Server {
	return newRpcServer(opt...)
}

// NewSubscriber creates a new subscriber interface with the given topic
// and handler using the default server
func NewSubscriber(topic string, h interface{}, opts ...SubscriberOption) Subscriber {
	return DefaultServer.NewSubscriber(topic, h, opts...)
}

// NewHandler creates a new handler interface using the default server
// Handlers are required to be a public object with public
// endpoints. Call to a service endpoint such as Foo.Bar expects
// the type:
//
//	type Foo struct {}
//	func (f *Foo) Bar(ctx, req, rsp) error {
//		return nil
//	}
//
func NewHandler(h interface{}, opts ...HandlerOption) Handler {
	return DefaultServer.NewHandler(h, opts...)
}

// Handle registers a handler interface with the default server to
// handle inbound requests
func Handle(h Handler) error {
	return DefaultServer.Handle(h)
}

// Subscribe registers a subscriber interface with the default server
// which subscribes to specified topic with the broker
func Subscribe(s Subscriber) error {
	return DefaultServer.Subscribe(s)
}

// Run starts the default server and waits for a kill
// signal before exiting. Also registers/deregisters the server
func Run() error {
	if err := Start(); err != nil {
		return err
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT, syscall.SIGKILL)
	log.Logf("Received signal %s", <-ch)

	return Stop()
}

// Start starts the default server
func Start() error {
	config := DefaultServer.Options()
	log.Logf("Starting server %s id %s", config.Name, config.Id)
	return DefaultServer.Start()
}

// Stop stops the default server
func Stop() error {
	log.Logf("Stopping server")
	return DefaultServer.Stop()
}

// String returns name of Server implementation
func String() string {
	return DefaultServer.String()
}
