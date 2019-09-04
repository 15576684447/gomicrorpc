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

func (m *mdnsRegistry) Register(service *Service, opts ...RegisterOption) error {
	m.Lock()
	defer m.Unlock()
	//检查服务是否已经存在，不存在则新建mdns服务节点
	entries, ok := m.services[service.Name]
	// first entry, create wildcard used for list queries
	if !ok {
		//创建节点，检查参数，如果参数可选并且缺失，则使用默认参数
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
		//添加mdnsEntry，此时不指定id，后面指定
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
		//查看待注册的service节点在mdnsRegistry.services中是否已经存在
		for _, entry := range entries {
			if node.Id == entry.id {
				seen = true
				e = entry
				break
			}
		}

		// already registered, continue
		//如果存在并且id的hash值也一样，说明是同一个服务，跳过该node检查
		if seen && e.hash == h {
			continue
			// hash doesn't match, shutdown
			//如果仅仅是id相同，但是hash值不同，说明不匹配，关闭旧的node
		} else if seen {
			e.node.Shutdown()
			// doesn't exist
		} else {
			//如果是新建的mdnsEntry节点，因为初始指定为*，所以会在此处统一指定hash id值
			e = &mdnsEntry{hash: h}
		}
		//encode服务信息进mdnsTxt
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
		//新建一个mdns 服务节点
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
		//注册新的mdns服务
		srv, err := mdns.NewServer(&mdns.Config{Zone: s})
		if err != nil {
			gerr = err
			continue
		}

		e.id = node.Id
		e.node = srv
		//向该服务内新加一个mdnsEntry
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
			if node.Id == entry.id {
				entry.node.Shutdown()
				remove = true
				break
			}
		}

		// keep it?
		if !remove {
			newEntries = append(newEntries, entry)
		}
	}

	// last entry is the wildcard for list queries. Remove it.
	if len(newEntries) == 1 && newEntries[0].id == "*" {
		newEntries[0].node.Shutdown()
		delete(m.services, service.Name)
	} else {
		m.services[service.Name] = newEntries
	}

	return nil
}

func (m *mdnsRegistry) GetService(service string) ([]*Service, error) {
	serviceMap := make(map[string]*Service)
	entries := make(chan *mdns.ServiceEntry, 10)
	done := make(chan bool)

	p := mdns.DefaultParams(service)
	// set context with timeout
	p.Context, _ = context.WithTimeout(context.Background(), m.opts.Timeout)
	// set entries channel
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

				s, ok := serviceMap[txt.Version]
				if !ok {
					s = &Service{
						Name:      txt.Service,
						Version:   txt.Version,
						Endpoints: txt.Endpoints,
					}
				}

				s.Nodes = append(s.Nodes, &Node{
					Id:       strings.TrimSuffix(e.Name, "."+p.Service+"."+p.Domain+"."),
					Address:  fmt.Sprintf("%s:%d", e.AddrV4.String(), e.Port),
					Metadata: txt.Metadata,
				})

				serviceMap[txt.Version] = s
			case <-p.Context.Done():
				close(done)
				return
			}
		}
	}()

	// execute the query
	if err := mdns.Query(p); err != nil {
		return nil, err
	}

	// wait for completion
	<-done

	// create list and return
	var services []*Service

	for _, service := range serviceMap {
		services = append(services, service)
	}

	return services, nil
}

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
