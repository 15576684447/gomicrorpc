package consul

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"time"

	consul "github.com/hashicorp/consul/api"
	"github.com/micro/go-micro/registry"
	hash "github.com/mitchellh/hashstructure"
)

type consulRegistry struct {
	Address string
	Client  *consul.Client
	opts    registry.Options

	// connect enabled
	connect bool

	queryOptions *consul.QueryOptions

	sync.Mutex
	register map[string]uint64
	// lastChecked tracks when a node was last checked as existing in Consul
	lastChecked map[string]time.Time
}

func getDeregisterTTL(t time.Duration) time.Duration {
	// splay slightly for the watcher?
	splay := time.Second * 5
	deregTTL := t + splay

	// consul has a minimum timeout on deregistration of 1 minute.
	if t < time.Minute {
		deregTTL = time.Minute + splay
	}

	return deregTTL
}

func newTransport(config *tls.Config) *http.Transport {
	if config == nil {
		config = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	t := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		Dial: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).Dial,
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig:     config,
	}
	runtime.SetFinalizer(&t, func(tr **http.Transport) {
		(*tr).CloseIdleConnections()
	})
	return t
}

func configure(c *consulRegistry, opts ...registry.Option) {
	// set opts
	for _, o := range opts {
		o(&c.opts)
	}

	// use default config
	config := consul.DefaultConfig()

	if c.opts.Context != nil {
		// Use the consul config passed in the options, if available
		if co, ok := c.opts.Context.Value("consul_config").(*consul.Config); ok {
			config = co
		}
		if cn, ok := c.opts.Context.Value("consul_connect").(bool); ok {
			c.connect = cn
		}

		// Use the consul query options passed in the options, if available
		if qo, ok := c.opts.Context.Value("consul_query_options").(*consul.QueryOptions); ok && qo != nil {
			c.queryOptions = qo
		}
		if as, ok := c.opts.Context.Value("consul_allow_stale").(bool); ok {
			c.queryOptions.AllowStale = as
		}
	}

	// check if there are any addrs
	if len(c.opts.Addrs) > 0 {
		//[ip]:port或者ip:port
		addr, port, err := net.SplitHostPort(c.opts.Addrs[0])
		//如果只是缺少port,指定默认port为8500
		if ae, ok := err.(*net.AddrError); ok && ae.Err == "missing port in address" {
			port = "8500"
			addr = c.opts.Addrs[0]
			config.Address = fmt.Sprintf("%s:%s", addr, port)
		} else if err == nil {
			//否则使用指定的port
			config.Address = fmt.Sprintf("%s:%s", addr, port)
		}
	}

	if config.HttpClient == nil {
		config.HttpClient = new(http.Client)
	}

	// requires secure connection?
	//指定https传输方式
	if c.opts.Secure || c.opts.TLSConfig != nil {

		config.Scheme = "https"
		// We're going to support InsecureSkipVerify
		config.HttpClient.Transport = newTransport(c.opts.TLSConfig)
	}

	// set timeout
	//http设置超时
	if c.opts.Timeout > 0 {
		config.HttpClient.Timeout = c.opts.Timeout
	}

	// create the client
	//创建Client
	//默认Client配置如下：
	//使用HTTP传输，并且服务器地址为127.0.0.1:8500
	client, _ := consul.NewClient(config)

	// set address/client
	//设置consulRegistry的Address与Client参数
	c.Address = config.Address
	c.Client = client
}

func (c *consulRegistry) Init(opts ...registry.Option) error {
	configure(c, opts...)
	return nil
}

func (c *consulRegistry) Deregister(s *registry.Service) error {
	if len(s.Nodes) == 0 {
		return errors.New("Require at least one node")
	}

	// delete our hash and time check of the service
	//从map中删除服务与健康检查时间
	c.Lock()
	delete(c.register, s.Name)
	delete(c.lastChecked, s.Name)
	c.Unlock()
	//同时从consul注销服务
	node := s.Nodes[0]
	return c.Client.Agent().ServiceDeregister(node.Id)
}

func (c *consulRegistry) Register(s *registry.Service, opts ...registry.RegisterOption) error {
	if len(s.Nodes) == 0 {
		return errors.New("Require at least one node")
	}

	var regTCPCheck bool
	var regInterval time.Duration

	var options registry.RegisterOptions
	for _, o := range opts {
		o(&options)
	}
	//从ctx中获取checkInterval参数
	if c.opts.Context != nil {
		if tcpCheckInterval, ok := c.opts.Context.Value("consul_tcp_check").(time.Duration); ok {
			regTCPCheck = true
			regInterval = tcpCheckInterval
		}
	}

	// create hash of service; uint64
	//哈希化Service
	h, err := hash.Hash(s, nil)
	if err != nil {
		return err
	}

	// use first node
	node := s.Nodes[0]

	// get existing hash and last checked time
	c.Lock()
	//查看服务是否已经存在
	v, ok := c.register[s.Name]
	//查看服务最近一次Checked
	lastChecked := c.lastChecked[s.Name]
	c.Unlock()

	// if it's already registered and matches then just pass the check
	//如果服务已经注册
	if ok && v == h {
		//如果未开启定时健康检查，则需要手动确认下服务是否健康
		if options.TTL == time.Duration(0) {
			// ensure that our service hasn't been deregistered by Consul
			//获取注销TTL，最少为1分钟，查看服务距离上次Check间隔是否大于该TTL，如果还没超出间隔，说明服务健康，直接返回
			if time.Since(lastChecked) <= getDeregisterTTL(regInterval) {
				return nil
			}
			//如果服务距离上次Check间隔大于注销TTL，进行健康检查一次
			services, _, err := c.Client.Health().Checks(s.Name, c.queryOptions)
			//如果检查通过，且服务中包含本次待注册的服务，返回
			if err == nil {
				for _, v := range services {
					if v.ServiceID == node.Id {
						return nil
					}
				}
			}
		} else {
			// if the err is nil we're all good, bail out
			// if not, we don't know what the state is, so full re-register
			//如果之前使能健康检查，说明此时服务健康，无需再次检查，将服务的TTL周期检查设置为通过
			//如果设置出错，那么此时服务状态是未知的，则需要重新注册该服务，继续往下走
			if err := c.Client.Agent().PassTTL("service:"+node.Id, ""); err == nil {
				return nil
			}
		}
	}

	// encode the tags
	//编码，json化后再编码
	tags := encodeMetadata(node.Metadata)
	tags = append(tags, encodeEndpoints(s.Endpoints)...)
	tags = append(tags, encodeVersion(s.Version)...)

	var check *consul.AgentServiceCheck
	//如果使能了Check
	if regTCPCheck {
		//获取注销TTL
		deregTTL := getDeregisterTTL(regInterval)
		//创建Check参数
		check = &consul.AgentServiceCheck{
			TCP:                            node.Address,
			Interval:                       fmt.Sprintf("%v", regInterval),
			DeregisterCriticalServiceAfter: fmt.Sprintf("%v", deregTTL),
		}

		// if the TTL is greater than 0 create an associated check
		//如果TTL选项>0，也创建对应的Check参数
	} else if options.TTL > time.Duration(0) {
		deregTTL := getDeregisterTTL(options.TTL)

		check = &consul.AgentServiceCheck{
			TTL:                            fmt.Sprintf("%v", options.TTL),
			DeregisterCriticalServiceAfter: fmt.Sprintf("%v", deregTTL),
		}
	}

	host, pt, _ := net.SplitHostPort(node.Address)
	port, _ := strconv.Atoi(pt)

	// register the service
	//初始化服务注册结构体
	asr := &consul.AgentServiceRegistration{
		ID:      node.Id,
		Name:    s.Name,
		Tags:    tags,
		Port:    port,
		Address: host,
		Check:   check,
	}

	// Specify consul connect
	if c.connect {
		asr.Connect = &consul.AgentServiceConnect{
			Native: true,
		}
	}
	//正式注册服务到consul
	if err := c.Client.Agent().ServiceRegister(asr); err != nil {
		return err
	}

	// save our hash and time check of the service
	c.Lock()
	//将服务以及最近一次Check时间更新到consulRegistry结构体中
	c.register[s.Name] = h
	c.lastChecked[s.Name] = time.Now()
	c.Unlock()

	// if the TTL is 0 we don't mess with the checks
	//如果TTL为0，则默认不检查，返回
	if options.TTL == time.Duration(0) {
		return nil
	}

	// pass the healthcheck
	//否则，设置服务状态为健康
	return c.Client.Agent().PassTTL("service:"+node.Id, "")
}
//获取所有服务，帅选出名字为name的服务，剔除那些不健康的节点并返回
func (c *consulRegistry) GetService(name string) ([]*registry.Service, error) {
	var rsp []*consul.ServiceEntry
	var err error

	// if we're connect enabled only get connect services
	if c.connect {//如果使能了连接，则获取那些通过健康检查且可连接的服务
		rsp, _, err = c.Client.Health().Connect(name, "", false, c.queryOptions)
	} else {//否则，返回那些通过健康检查的服务
		rsp, _, err = c.Client.Health().Service(name, "", false, c.queryOptions)
	}
	if err != nil {
		return nil, err
	}

	serviceMap := map[string]*registry.Service{}
	//遍历所有返回的服务，找到名字为name的服务
	for _, s := range rsp {
		if s.Service.Service != name {
			continue
		}

		// version is now a tag
		//从Tag中解析version，同时也当作key值
		version, _ := decodeVersion(s.Service.Tags)
		// service ID is now the node id
		id := s.Service.ID
		// key is always the version
		key := version

		// address is service address
		address := s.Service.Address

		// use node address
		if len(address) == 0 {
			address = s.Node.Address
		}
		//如果服务已经存在，则获取服务；否则，新建服务，将svc加入到serviceMap中
		svc, ok := serviceMap[key]
		if !ok {
			svc = &registry.Service{
				//从Tag中解析出EndPoints参数
				Endpoints: decodeEndpoints(s.Service.Tags),
				Name:      s.Service.Service,
				Version:   version,
			}
			serviceMap[key] = svc
		}

		var del bool
		//去除那些status=critical的节点
		for _, check := range s.Checks {
			// delete the node if the status is critical
			if check.Status == "critical" {
				del = true
				break
			}
		}

		// if delete then skip the node
		if del {
			continue
		}
		//如果节点健康，才加入到service的节点中
		svc.Nodes = append(svc.Nodes, &registry.Node{
			Id:       id,
			Address:  fmt.Sprintf("%s:%d", address, s.Service.Port),
			//从Tag中解析Metadata
			Metadata: decodeMetadata(s.Service.Tags),
		})
	}
	//整理所有service
	var services []*registry.Service
	for _, service := range serviceMap {
		services = append(services, service)
	}
	return services, nil
}
//返回所有服务，不管是否健康，这里只获取了服务名列表
func (c *consulRegistry) ListServices() ([]*registry.Service, error) {
	rsp, _, err := c.Client.Catalog().Services(c.queryOptions)
	if err != nil {
		return nil, err
	}

	var services []*registry.Service

	for service := range rsp {
		services = append(services, &registry.Service{Name: service})
	}

	return services, nil
}

func (c *consulRegistry) Watch(opts ...registry.WatchOption) (registry.Watcher, error) {
	return newConsulWatcher(c, opts...)
}

func (c *consulRegistry) String() string {
	return "consul"
}

func (c *consulRegistry) Options() registry.Options {
	return c.opts
}

func NewRegistry(opts ...registry.Option) registry.Registry {
	cr := &consulRegistry{
		opts:        registry.Options{},
		register:    make(map[string]uint64),
		lastChecked: make(map[string]time.Time),
		queryOptions: &consul.QueryOptions{
			AllowStale: true,
		},
	}
	configure(cr, opts...)
	return cr
}
