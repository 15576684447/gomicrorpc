// Package mdns is a multicast dns registry
package registry

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/micro/mdns"
	hash "github.com/mitchellh/hashstructure"
)

type mdnsTxt struct {
	Service   string
	Version   string
	Endpoints []*Endpoint
	Metadata  map[string]string
}

type mdnsEntry struct {
	hash uint64
	id   string
	node *mdns.Server
}

type mdnsRegistry struct {
	opts Options

	sync.Mutex
	services map[string][]*mdnsEntry
}

func newRegistry(opts ...Option) Registry {
	options := Options{
		Timeout: time.Millisecond * 100,
	}

	return &mdnsRegistry{
		opts:     options,
		services: make(map[string][]*mdnsEntry),
	}
}

func (m *mdnsRegistry) Init(opts ...Option) error {
	for _, o := range opts {
		o(&m.opts)
	}
	return nil
}

func (m *mdnsRegistry) Options() Options {
	return m.opts
}
//注册mdns服务，服务以mdnsEntry结构存在，主要有一下特性
//mdnsEntry结构中必须包含一个主节点，专门用于广播入口；另外每个service node生成一个属于自己的entry，加入到mdnsEntry中
//主节点的id为*，service node自节点id为node的hash值
//当注销服务的mdnsEntry时，如果最后只剩一个entry且id为*，说明只剩主节点，也要一并删除，代表该服务不再存在
func (m *mdnsRegistry) Register(service *Service, opts ...RegisterOption) error {
	m.Lock()
	defer m.Unlock()
	//检查服务是否已经存在，不存在则新建mdns服务节点
	entries, ok := m.services[service.Name]
	// first entry, create wildcard used for list queries
	if !ok {
		//创建服务Entry节点，如果是非具体服务，IP=0.0.0.0,PORT=9999 ???
		s, err := mdns.NewMDNSService(
			service.Name,
			"_services",
			"",
			"",
			9999,
			[]net.IP{net.ParseIP("0.0.0.0")},
			nil,
		)
		if err != nil {
			return err
		}
		//新建mdns服务器，建立IPV4或者IPV6组内监听，接收组内广播或者单播，根据包的类型区分；并且通过probe函数向组内发送本服务信息
		srv, err := mdns.NewServer(&mdns.Config{Zone: &mdns.DNSSDService{s}})
		if err != nil {
			return err
		}

		// append the wildcard entry
		//先添加广播入口至mdnsEntry，其id固定为*
		entries = append(entries, &mdnsEntry{id: "*", node: srv})
	}

	var gerr error
	//遍历该服务的Nodes
	for _, node := range service.Nodes {
		// create hash of service; uint64
		//hash化node
		h, err := hash.Hash(node, nil)
		if err != nil {
			gerr = err
			continue
		}

		var seen bool
		var e *mdnsEntry
		//查看该服务的entries是否已经包含了待注册的node
		for _, entry := range entries {
			if node.Id == entry.id {
				seen = true
				e = entry
				break
			}
		}

		// already registered, continue
		//如果存在并且id的hash值也一样，说明是同一个服务，跳过该node的添加
		if seen && e.hash == h {
			continue
			// hash doesn't match, shutdown
			//如果仅仅是id相同，但是node的hash值不同，说明不匹配，关闭旧的node；使用新的替代
		} else if seen {
			//注销服务
			e.node.Shutdown()
			// doesn't exist
		} else {
			//如果是新的node节点，添加新节点，首先初始化的就是其hash值字段
			e = &mdnsEntry{hash: h}
		}
		//将服务具体信息编码成TXT，TXT在mDNS作为附加信息存在
		txt, err := encode(&mdnsTxt{
			Service:   service.Name,
			Version:   service.Version,
			Endpoints: service.Endpoints,
			Metadata:  node.Metadata,
		})

		if err != nil {
			gerr = err
			continue
		}

		//取出ip和port
		host, pt, err := net.SplitHostPort(node.Address)
		if err != nil {
			gerr = err
			continue
		}
		port, _ := strconv.Atoi(pt)

		// we got here, new node
		//具体服务信息，包括服务名，IP，port以及服务具体信息txt
		s, err := mdns.NewMDNSService(
			node.Id,
			service.Name,
			"",
			"",
			port,
			[]net.IP{net.ParseIP(host)},
			txt,
		)
		if err != nil {
			gerr = err
			continue
		}
		//将服务使用mDNS在局域网内注册，具体包括PTR，SRV，TXT部分
		srv, err := mdns.NewServer(&mdns.Config{Zone: s})
		if err != nil {
			gerr = err
			continue
		}

		e.id = node.Id
		e.node = srv
		//向该服务的entries内新加一个节点
		entries = append(entries, e)
	}

	// save
	//保存服务到本地map
	m.services[service.Name] = entries

	return gerr
}

func (m *mdnsRegistry) Deregister(service *Service) error {
	m.Lock()
	defer m.Unlock()

	var newEntries []*mdnsEntry

	// loop existing entries, check if any match, shutdown those that do
	for _, entry := range m.services[service.Name] {
		var remove bool

		for _, node := range service.Nodes {
			//只注销与传入的service节点id相同的服务
			if node.Id == entry.id {
				entry.node.Shutdown()
				remove = true
				break
			}
		}

		// keep it?
		//否则保留
		if !remove {
			newEntries = append(newEntries, entry)
		}
	}

	// last entry is the wildcard for list queries. Remove it.
	//如果只剩最后一个节点且id为*，仅仅是广播请求作用的，没有具体服务，移除
	if len(newEntries) == 1 && newEntries[0].id == "*" {
		newEntries[0].node.Shutdown()
		delete(m.services, service.Name)
	} else {
		//否则将未注销的服务放回
		m.services[service.Name] = newEntries
	}

	return nil
}

func (m *mdnsRegistry) GetService(service string) ([]*Service, error) {
	serviceMap := make(map[string]*Service)
	entries := make(chan *mdns.ServiceEntry, 10)
	done := make(chan bool)
	//获取请求默认参数，并指定ctx超时参数以及服务传递管道
	//程序将Query执行结果传入管道，并在下面的程序中等待管道中的服务返回
	//并逐个验证，获取想要的服务，并返回
	p := mdns.DefaultParams(service)
	// set context with timeout
	p.Context, _ = context.WithTimeout(context.Background(), m.opts.Timeout)
	// set entries channel
	//消息传递管道，将该管道传递进Query函数，将Query结果通过管道传递出来，并在下面的线程中接收处理
	p.Entries = entries

	go func() {
		for {
			select {
			case e := <-entries:
				// list record so skip
				if p.Service == "_services" {
					continue
				}

				if e.TTL == 0 {
					continue
				}

				txt, err := decode(e.InfoFields)
				if err != nil {
					continue
				}

				if txt.Service != service {
					continue
				}
				//如果返回的service是想要获取的，则获取服务信息
				s, ok := serviceMap[txt.Version]
				if !ok {
					s = &Service{
						Name:      txt.Service,
						Version:   txt.Version,
						Endpoints: txt.Endpoints,
					}
				}
				//为服务添加节点
				s.Nodes = append(s.Nodes, &Node{
					Id:       strings.TrimSuffix(e.Name, "."+p.Service+"."+p.Domain+"."),
					Address:  fmt.Sprintf("%s:%d", e.AddrV4.String(), e.Port),
					Metadata: txt.Metadata,
				})
				//添加服务到表格
				serviceMap[txt.Version] = s
			case <-p.Context.Done():
				close(done)
				return
			}
		}
	}()

	// execute the query
	//执行请求服务，主要是在组内广播其他服务，等待其他服务返回
	if err := mdns.Query(p); err != nil {
		return nil, err
	}

	// wait for completion
	//结束以Context超时为准
	<-done

	// create list and return
	var services []*Service

	for _, service := range serviceMap {
		services = append(services, service)
	}

	return services, nil
}
//返回都有服务，与GetService逻辑类似
func (m *mdnsRegistry) ListServices() ([]*Service, error) {
	serviceMap := make(map[string]bool)
	entries := make(chan *mdns.ServiceEntry, 10)
	done := make(chan bool)

	p := mdns.DefaultParams("_services")
	// set context with timeout
	p.Context, _ = context.WithTimeout(context.Background(), m.opts.Timeout)
	// set entries channel
	p.Entries = entries

	var services []*Service

	go func() {
		for {
			select {
			case e := <-entries:
				if e.TTL == 0 {
					continue
				}

				name := strings.TrimSuffix(e.Name, "."+p.Service+"."+p.Domain+".")
				if !serviceMap[name] {
					serviceMap[name] = true
					services = append(services, &Service{Name: name})
				}
			case <-p.Context.Done():
				close(done)
				return
			}
		}
	}()

	// execute query
	if err := mdns.Query(p); err != nil {
		return nil, err
	}

	// wait till done
	<-done

	return services, nil
}

func (m *mdnsRegistry) Watch(opts ...WatchOption) (Watcher, error) {
	var wo WatchOptions
	for _, o := range opts {
		o(&wo)
	}

	md := &mdnsWatcher{
		wo:   wo,
		ch:   make(chan *mdns.ServiceEntry, 32),
		exit: make(chan struct{}),
	}

	go func() {
		if err := mdns.Listen(md.ch, md.exit); err != nil {
			md.Stop()
		}
	}()

	return md, nil
}

func (m *mdnsRegistry) String() string {
	return "mdns"
}

// NewRegistry returns a new default registry which is mdns
func NewRegistry(opts ...Option) Registry {
	return newRegistry(opts...)
}
