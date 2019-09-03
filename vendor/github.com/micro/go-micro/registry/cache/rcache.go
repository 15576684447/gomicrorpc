// Package cache provides a registry cache
package cache

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/micro/go-micro/registry"
	log "github.com/micro/go-micro/util/log"
)

// Cache is the registry cache interface
type Cache interface {
	// embed the registry interface
	registry.Registry
	// stop the cache watcher
	Stop()
}

type Options struct {
	// TTL is the cache TTL
	TTL time.Duration
}

type Option func(o *Options)
//本地cache缓存，将服务信息保存在本地cache，并根据watch结果更新缓存
//缓存内容有：注册的服务信息，服务生命周期ttl以及服务是否开启watch监听
type cache struct {
	registry.Registry
	opts Options

	// registry cache
	sync.RWMutex
	cache   map[string][]*registry.Service
	ttls    map[string]time.Time
	watched map[string]bool

	exit chan bool
}

var (
	DefaultTTL = time.Minute
)
//返回延迟参数，每次以10^n方增加，n为次数
func backoff(attempts int) time.Duration {
	if attempts == 0 {
		return time.Duration(0)
	}
	return time.Duration(math.Pow(10, float64(attempts))) * time.Millisecond
}

// isValid checks if the service is valid
func (c *cache) isValid(services []*registry.Service, ttl time.Time) bool {
	// no services exist
	if len(services) == 0 {
		return false
	}

	// ttl is invalid
	if ttl.IsZero() {
		return false
	}

	// time since ttl is longer than timeout
	if time.Since(ttl) > c.opts.TTL {
		return false
	}

	// ok
	return true
}

func (c *cache) quit() bool {
	select {
	case <-c.exit:
		return true
	default:
		return false
	}
}
//删除map中的节点
func (c *cache) del(service string) {
	delete(c.cache, service)
	delete(c.ttls, service)
}
//从cache的map中获取服务
func (c *cache) get(service string) ([]*registry.Service, error) {
	// read lock
	c.RLock()

	// check the cache first
	//获取服务和ttl信息
	services := c.cache[service]
	// get cache ttl
	ttl := c.ttls[service]

	// got services && within ttl so return cache
	//service不为空，且ttl在有效期内
	if c.isValid(services, ttl) {
		// make a copy
		cp := registry.Copy(services)
		// unlock the read
		c.RUnlock()
		// return servics
		return cp, nil
	}

	// get does the actual request for a service and cache it
	//如果cache中不存在对应的服务或者服务已经失效，则请求获取对应服务并更新到cache
	get := func(service string) ([]*registry.Service, error) {
		// ask the registry
		services, err := c.Registry.GetService(service)
		if err != nil {
			return nil, err
		}

		// cache results
		c.Lock()
		c.set(service, registry.Copy(services))
		c.Unlock()

		return services, nil
	}

	// watch service if not watched
	//查看对应服务是否watch状态为enable，如果不是，启动线程watch服务
	if _, ok := c.watched[service]; !ok {
		go c.run(service)
	}

	// unlock the read lock
	c.RUnlock()

	// get and return services
	//返回从服务器获取的服务
	return get(service)
}
//记录服务与ttl，ttl为当前时间加上ttl间隔
func (c *cache) set(service string, services []*registry.Service) {
	c.cache[service] = services
	c.ttls[service] = time.Now().Add(c.opts.TTL)
}
//在缓存中执行服务更新操作，包括增、删、改等操作
func (c *cache) update(res *registry.Result) {
	if res == nil || res.Service == nil {
		return
	}

	c.Lock()
	defer c.Unlock()

	services, ok := c.cache[res.Service.Name]
	if !ok {
		// we're not going to cache anything
		// unless there was already a lookup
		return
	}
	//如果Service节点为空，并且执行delete参数，从缓存中删除对应的服务；否则直接返回
	if len(res.Service.Nodes) == 0 {
		switch res.Action {
		case "delete":
			c.del(res.Service.Name)
		}
		return
	}

	// existing service found
	var service *registry.Service
	var index int
	//从cache中找出对应name对应version的服务，做备份并记录index
	for i, s := range services {
		if s.Version == res.Service.Version {
			service = s
			index = i
		}
	}

	switch res.Action {
	case "create", "update":
		//如果新服务在cache中不存在，直接将整个服务添加进cache
		if service == nil {
			c.set(res.Service.Name, append(services, res.Service))
			return
		}

		// append old nodes to new service
		//否则只更新服务的节点信息，遍历新服务节点，查看那些节点在cache中不存在
		for _, cur := range service.Nodes {
			var seen bool
			for _, node := range res.Service.Nodes {
				//如果新服务节点在cache中已经存在，跳过，无需添加
				if cur.Id == node.Id {
					seen = true
					break
				}
			}
			//否则添加需要增加并且之前没有的节点
			if !seen {
				res.Service.Nodes = append(res.Service.Nodes, cur)
			}
		}

		services[index] = res.Service
		c.set(res.Service.Name, services)
	case "delete":
		if service == nil {
			return
		}

		var nodes []*registry.Node

		// filter cur nodes to remove the dead one
		//遍历新服务(需要删除的)节点，查看cache中那些Node需要被删除
		for _, cur := range service.Nodes {
			var seen bool
			//cache中一旦有id相同的，说明需要被删除，则不用保留，直接跳过
			for _, del := range res.Service.Nodes {
				if del.Id == cur.Id {
					seen = true
					break
				}
			}
			if !seen {
				//将不需要删除的节点保留
				nodes = append(nodes, cur)
			}
		}

		// still got nodes, save and return
		//保留不需要删除的节点，放回cache
		if len(nodes) > 0 {
			service.Nodes = nodes
			services[index] = service
			c.set(service.Name, services)
			return
		}

		// zero nodes left

		// only have one thing to delete
		// nuke the thing
		//如果服务已经没有节点，删除服务
		if len(services) == 1 {
			c.del(service.Name)
			return
		}

		// still have more than 1 service
		// check the version and keep what we know
		var srvs []*registry.Service
		//如果存在一个以上同名服务，保留version不同的服务，重新放回cache
		for _, s := range services {
			if s.Version != service.Version {
				srvs = append(srvs, s)
			}
		}

		// save
		c.set(service.Name, srvs)
	}
}

// run starts the cache watcher loop
// it creates a new watcher if there's a problem
//watch线程，时刻监听服务变化
func (c *cache) run(service string) {
	// set watcher
	c.Lock()
	//设置对应服务watch状态为true
	c.watched[service] = true
	c.Unlock()

	// delete watcher on exit
	defer func() {
		c.Lock()
		delete(c.watched, service)
		c.Unlock()
	}()

	var a, b int

	for {
		// exit early if already dead
		if c.quit() {
			return
		}

		// jitter before starting
		//随机延时
		j := rand.Int63n(100)
		time.Sleep(time.Duration(j) * time.Millisecond)

		// create new watcher
		//为该服务新建一个Registry.Watch，并指定watch的服务名选项
		w, err := c.Registry.Watch(
			registry.WatchService(service),
		)
		//如果Registry.Watch注册失败，则循环尝试，3次一个轮回，第一次等到0ms后尝试，第一次等待10ms后尝试，第二次等到100ms后重试
		if err != nil {
			if c.quit() {
				return
			}

			d := backoff(a)

			if a > 3 {
				log.Log("rcache: ", err, " backing off ", d)
				a = 0
			}

			time.Sleep(d)
			a++

			continue
		}

		// reset a
		a = 0

		// watch for events
		//监听服务，循环等待，等待从registry.Result获取数据，并在本地cache更新
		if err := c.watch(w); err != nil {
			if c.quit() {
				return
			}

			d := backoff(b)

			if b > 3 {
				log.Log("rcache: ", err, " backing off ", d)
				b = 0
			}

			time.Sleep(d)
			b++

			continue
		}

		// reset b
		b = 0
	}
}

// watch loops the next event and calls update
// it returns if there's an error
//当registry.Watcher有服务更新后，会在缓存中同步更新，cache的watch函数一直循环等待，而Next函数是阻塞型的
func (c *cache) watch(w registry.Watcher) error {
	defer w.Stop()

	// manage this loop
	go func() {
		// wait for exit
		<-c.exit
		w.Stop()
	}()

	for {
		//Next函数是阻塞函数，管道中有数据才会返回，此时必然是服务需要更新
		res, err := w.Next()
		if err != nil {
			return err
		}
		c.update(res)
	}
}
//获取对应名字的服务
func (c *cache) GetService(service string) ([]*registry.Service, error) {
	// get the service
	services, err := c.get(service)
	if err != nil {
		return nil, err
	}

	// if there's nothing return err
	if len(services) == 0 {
		return nil, registry.ErrNotFound
	}

	// return services
	return services, nil
}

func (c *cache) Stop() {
	select {
	case <-c.exit:
		return
	default:
		close(c.exit)
	}
}

func (c *cache) String() string {
	return "rcache"
}

// New returns a new cache
func New(r registry.Registry, opts ...Option) Cache {
	rand.Seed(time.Now().UnixNano())
	options := Options{
		TTL: DefaultTTL,
	}

	for _, o := range opts {
		o(&options)
	}

	return &cache{
		Registry: r,
		opts:     options,
		watched:  make(map[string]bool),
		cache:    make(map[string][]*registry.Service),
		ttls:     make(map[string]time.Time),
		exit:     make(chan bool),
	}
}
