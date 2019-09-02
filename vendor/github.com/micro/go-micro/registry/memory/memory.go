// Package memory provides an in-memory registry
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/micro/go-micro/registry"
)

type Registry struct {
	options registry.Options

	sync.RWMutex
	Services map[string][]*registry.Service
	Watchers map[string]*Watcher
}

var (
	timeout = time.Millisecond * 10
)
//监听服务同步的服务信息
func (m *Registry) watch(r *registry.Result) {
	var watchers []*Watcher

	m.RLock()
	for _, w := range m.Watchers {
		watchers = append(watchers, w)
	}
	m.RUnlock()

	for _, w := range watchers {
		select {
		//如果Watchers已经退出，删除Watchers
		case <-w.exit:
			m.Lock()
			delete(m.Watchers, w.id)
			m.Unlock()
		default:
			//将新的registry.Result发送到(registry.Result)管道---更新到本地缓存？
			select {
			case w.res <- r:
			case <-time.After(timeout):
			}
		}
	}
}

func (m *Registry) Init(opts ...registry.Option) error {
	for _, o := range opts {
		o(&m.options)
	}

	// add services
	m.Lock()
	//从ctx中获取 map[string][]*registry.Service
	for k, v := range getServices(m.options.Context) {
		//获取map中原来的值
		s := m.Services[k]
		//将原值与从ctx中获取的新值进行融合
		m.Services[k] = registry.Merge(s, v)
	}
	m.Unlock()
	return nil
}

func (m *Registry) Options() registry.Options {
	return m.options
}
//根据Service name获取对应Service
func (m *Registry) GetService(name string) ([]*registry.Service, error) {
	m.RLock()
	service, ok := m.Services[name]
	m.RUnlock()
	if !ok {
		return nil, registry.ErrNotFound
	}

	return service, nil
}
//列举所有服务
func (m *Registry) ListServices() ([]*registry.Service, error) {
	var services []*registry.Service
	m.RLock()
	for _, service := range m.Services {
		services = append(services, service...)
	}
	m.RUnlock()
	return services, nil
}

func (m *Registry) Register(s *registry.Service, opts ...registry.RegisterOption) error {
	//更新Registry.Watcher, 添加新的Service
	go m.watch(&registry.Result{Action: "update", Service: s})
	m.Lock()
	//如果Services不存在，则新建节点
	if service, ok := m.Services[s.Name]; !ok {
		m.Services[s.Name] = []*registry.Service{s}
	} else {//如果存在，则融合两个节点
		m.Services[s.Name] = registry.Merge(service, []*registry.Service{s})
	}
	m.Unlock()

	return nil
}
//取消服务注册
func (m *Registry) Deregister(s *registry.Service) error {
	//更新Registry.Watcher, 删除指定Service
	go m.watch(&registry.Result{Action: "delete", Service: s})

	m.Lock()
	//如果存在该Service，则取消注册该Service
	if service, ok := m.Services[s.Name]; ok {
		//如果取消注册节点成功，则删除map中对应的Service
		if service := registry.Remove(service, []*registry.Service{s}); len(service) == 0 {
			delete(m.Services, s.Name)
		} else {//如果取消注册不成功，将Service补充回去，以免造成注册信息与map信息不对称
			m.Services[s.Name] = service
		}
	}
	m.Unlock()

	return nil
}
//Watcher接: 客户端监听服务信息变化---服务端定时同步服务信息给各个客户端，客户端根据服务端同步的信息更新本地缓存
func (m *Registry) Watch(opts ...registry.WatchOption) (registry.Watcher, error) {
	var wo registry.WatchOptions
	for _, o := range opts {
		o(&wo)
	}

	w := &Watcher{
		exit: make(chan bool),
		res:  make(chan *registry.Result),//watch信息更新管道
		id:   uuid.New().String(),
		wo:   wo,
	}

	m.Lock()
	m.Watchers[w.id] = w
	m.Unlock()
	return w, nil
}

func (m *Registry) String() string {
	return "memory"
}

func NewRegistry(opts ...registry.Option) registry.Registry {
	options := registry.Options{
		Context: context.Background(),
	}

	for _, o := range opts {
		o(&options)
	}

	services := getServices(options.Context)
	if services == nil {
		services = make(map[string][]*registry.Service)
	}

	return &Registry{
		options:  options,
		Services: services,
		Watchers: make(map[string]*Watcher),
	}
}
