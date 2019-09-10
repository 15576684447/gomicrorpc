// Package gossip provides a gossip registry based on hashicorp/memberlist
package gossip

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/google/uuid"
	"github.com/hashicorp/memberlist"
	"github.com/micro/go-micro/registry"
	pb "github.com/micro/go-micro/registry/gossip/proto"
	log "github.com/micro/go-micro/util/log"
	"github.com/mitchellh/hashstructure"
)

// use registry.Result int32 values after it switches from string to int32 types
// type actionType int32
// type updateType int32

const (
	actionTypeInvalid int32 = iota
	actionTypeCreate
	actionTypeDelete
	actionTypeUpdate
	actionTypeSync
)

const (
	nodeActionUnknown int32 = iota
	nodeActionJoin
	nodeActionLeave
	nodeActionUpdate
)

func actionTypeString(t int32) string {
	switch t {
	case actionTypeCreate:
		return "create"
	case actionTypeDelete:
		return "delete"
	case actionTypeUpdate:
		return "update"
	case actionTypeSync:
		return "sync"
	}
	return "invalid"
}

const (
	updateTypeInvalid int32 = iota
	updateTypeService
)

type broadcast struct {
	update *pb.Update
	notify chan<- struct{}
}

type delegate struct {
	queue   *memberlist.TransmitLimitedQueue
	updates chan *update
}

type event struct {
	action int32
	node   string
}

type eventDelegate struct {
	events chan *event
}

func (ed *eventDelegate) NotifyJoin(n *memberlist.Node) {
	ed.events <- &event{action: nodeActionJoin, node: n.Address()}
}
func (ed *eventDelegate) NotifyLeave(n *memberlist.Node) {
	ed.events <- &event{action: nodeActionLeave, node: n.Address()}
}
func (ed *eventDelegate) NotifyUpdate(n *memberlist.Node) {
	ed.events <- &event{action: nodeActionUpdate, node: n.Address()}
}

type gossipRegistry struct {
	//消息队列，用于将消息广播至cluster，但是限制消息单次广播的节点数量
	queue       *memberlist.TransmitLimitedQueue
	//update消息同步管道
	updates     chan *update
	//事件传输管道，解析后将其附加到members列表中
	events      chan *event
	options     registry.Options
	member      *memberlist.Memberlist
	interval    time.Duration
	tcpInterval time.Duration

	connectRetry   bool
	connectTimeout time.Duration
	sync.RWMutex
	//服务注册登记表
	services map[string][]*registry.Service
	//用于publish与subscribe方法中，将服务同步到本地cache
	watchers map[string]chan *registry.Result

	mtu     int
	//服务地址列表
	addrs   []string
	//保存解析后的事件
	members map[string]int32
	done    chan bool
}

type update struct {
	Update  *pb.Update
	Service *registry.Service
	sync    chan *registry.Service
}

type updates struct {
	sync.RWMutex
	services map[uint64]*update
}

var (
	// You should change this if using secure
	DefaultSecret = []byte("micro-gossip-key") // exactly 16 bytes
	ExpiryTick    = time.Second * 1            // needs to be smaller than registry.RegisterTTL
	MaxPacketSize = 512
)

func configure(g *gossipRegistry, opts ...registry.Option) error {
	// loop through address list and get valid entries
	addrs := func(curAddrs []string) []string {
		var newAddrs []string
		for _, addr := range curAddrs {
			if trimAddr := strings.TrimSpace(addr); len(trimAddr) > 0 {
				newAddrs = append(newAddrs, trimAddr)
			}
		}
		return newAddrs
	}

	// current address list
	//从addrs中获取有效地址
	curAddrs := addrs(g.options.Addrs)

	// parse options
	for _, o := range opts {
		o(&g.options)
	}

	// new address list
	//从设置后的地址中获取有效地址
	newAddrs := addrs(g.options.Addrs)

	// no new nodes and existing member. no configure
	//如果没有新地址，返回
	if (len(newAddrs) == len(curAddrs)) && g.member != nil {
		return nil
	}

	// shutdown old member
	//关闭旧的member
	g.Stop()

	// lock internals
	g.Lock()

	// new done chan
	g.done = make(chan bool)

	// replace addresses
	curAddrs = newAddrs

	// create a new default config
	//新建默认config
	c := memberlist.DefaultLocalConfig()

	// sane good default options
	c.LogOutput = ioutil.Discard // log to /dev/null
	c.PushPullInterval = 0       // disable expensive tcp push/pull
	c.ProtocolVersion = 4        // suport latest stable features

	// set config from options
	//如果ctx中传入config，则使用ctx配置
	if config, ok := g.options.Context.Value(configKey{}).(*memberlist.Config); ok && config != nil {
		c = config
	}

	// set address
	if address, ok := g.options.Context.Value(addressKey{}).(string); ok {
		host, port, err := net.SplitHostPort(address)
		if err == nil {
			p, err := strconv.Atoi(port)
			if err == nil {
				c.BindPort = p
			}
			c.BindAddr = host
		}
	} else {
		// set bind to random port
		c.BindPort = 0
	}

	// set the advertise address
	if advertise, ok := g.options.Context.Value(advertiseKey{}).(string); ok {
		host, port, err := net.SplitHostPort(advertise)
		if err == nil {
			p, err := strconv.Atoi(port)
			if err == nil {
				c.AdvertisePort = p
			}
			c.AdvertiseAddr = host
		}
	}

	// machine hostname
	hostname, _ := os.Hostname()

	// set the name
	c.Name = strings.Join([]string{"micro", hostname, uuid.New().String()}, "-")

	// set a secret key if secure
	if g.options.Secure {
		k, ok := g.options.Context.Value(secretKey{}).([]byte)
		if !ok {
			// use the default secret
			k = DefaultSecret
		}
		c.SecretKey = k
	}

	// set connect retry
	if v, ok := g.options.Context.Value(connectRetryKey{}).(bool); ok && v {
		g.connectRetry = true
	}

	// set connect timeout
	if td, ok := g.options.Context.Value(connectTimeoutKey{}).(time.Duration); ok {
		g.connectTimeout = td
	}

	// create a queue
	//queue的node数量由curAddrs总数量决定
	queue := &memberlist.TransmitLimitedQueue{
		NumNodes: func() int {
			return len(curAddrs)
		},
		RetransmitMult: 3,
	}

	// set the delegate
	//设置成员
	c.Delegate = &delegate{
		updates: g.updates,
		queue:   queue,
	}

	if g.connectRetry {
		//设置event成员
		c.Events = &eventDelegate{
			events: g.events,
		}
	}
	// create the memberlist
	//从config中创建memberlist
	m, err := memberlist.Create(c)
	if err != nil {
		return err
	}
	//为每个member设置地址
	if len(curAddrs) > 0 {
		for _, addr := range curAddrs {
			g.members[addr] = nodeActionUnknown
		}
	}

	g.tcpInterval = c.PushPullInterval
	g.addrs = curAddrs
	g.queue = queue
	//初始化成员
	g.member = m
	g.interval = c.GossipInterval

	g.Unlock()

	log.Logf("[gossip] Registry Listening on %s", m.LocalNode().Address())

	// try connect
	return g.connect(curAddrs)
}

func (*broadcast) UniqueBroadcast() {}

func (b *broadcast) Invalidates(other memberlist.Broadcast) bool {
	return false
}

func (b *broadcast) Message() []byte {
	up, err := proto.Marshal(b.update)
	if err != nil {
		return nil
	}
	if l := len(up); l > MaxPacketSize {
		log.Logf("[gossip] broadcast message size %d bigger then MaxPacketSize %d", l, MaxPacketSize)
	}
	return up
}

func (b *broadcast) Finished() {
	if b.notify != nil {
		close(b.notify)
	}
}

func (d *delegate) NodeMeta(limit int) []byte {
	return []byte{}
}

func (d *delegate) NotifyMsg(b []byte) {
	if len(b) == 0 {
		return
	}

	go func() {
		up := new(pb.Update)
		if err := proto.Unmarshal(b, up); err != nil {
			return
		}

		// only process service action
		if up.Type != updateTypeService {
			return
		}

		var service *registry.Service

		switch up.Metadata["Content-Type"] {
		case "application/json":
			if err := json.Unmarshal(up.Data, &service); err != nil {
				return
			}
		// no other content type
		default:
			return
		}

		// send update
		d.updates <- &update{
			Update:  up,
			Service: service,
		}
	}()
}

func (d *delegate) GetBroadcasts(overhead, limit int) [][]byte {
	return d.queue.GetBroadcasts(overhead, limit)
}

func (d *delegate) LocalState(join bool) []byte {
	if !join {
		return []byte{}
	}

	syncCh := make(chan *registry.Service, 1)
	services := map[string][]*registry.Service{}

	d.updates <- &update{
		Update: &pb.Update{
			Action: actionTypeSync,
		},
		sync: syncCh,
	}

	for srv := range syncCh {
		services[srv.Name] = append(services[srv.Name], srv)
	}

	b, _ := json.Marshal(services)
	return b
}

func (d *delegate) MergeRemoteState(buf []byte, join bool) {
	if len(buf) == 0 {
		return
	}
	if !join {
		return
	}

	var services map[string][]*registry.Service
	if err := json.Unmarshal(buf, &services); err != nil {
		return
	}
	for _, service := range services {
		for _, srv := range service {
			d.updates <- &update{
				Update:  &pb.Update{Action: actionTypeCreate},
				Service: srv,
				sync:    nil,
			}
		}
	}
}

func (g *gossipRegistry) connect(addrs []string) error {
	if len(addrs) == 0 {
		return nil
	}

	timeout := make(<-chan time.Time)

	if g.connectTimeout > 0 {
		timeout = time.After(g.connectTimeout)
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	fn := func() (int, error) {
		//该节点通过与其他所有节点建立联系，并同步数据，以此来加入cluster
		//函数返回建立连接的节点数与错误结果，如果返回错误，则加入cluster失败
		return g.member.Join(addrs)
	}

	// don't wait for first try
	//如果加入cluster成功，返回，否则继续
	if _, err := fn(); err == nil {
		return nil
	}

	// wait loop
	for {
		select {
		// context closed
		case <-g.options.Context.Done():
			return nil
		// call close, don't wait anymore
		case <-g.done:
			return nil
		//  in case of timeout fail with a timeout error
		case <-timeout:
			return fmt.Errorf("[gossip] connect timeout %v", g.addrs)
		// got a tick, try to connect
		case <-ticker.C:
			//每秒检查一次是否加入cluster成功
			if _, err := fn(); err == nil {
				log.Logf("[gossip] connect success for %v", g.addrs)
				return nil
			} else {
				log.Logf("[gossip] connect failed for %v", g.addrs)
			}
		}
	}

	return nil
}
//将消息发送至registry.Result管道
func (g *gossipRegistry) publish(action string, services []*registry.Service) {
	g.RLock()
	for _, sub := range g.watchers {
		go func(sub chan *registry.Result) {
			for _, service := range services {
				sub <- &registry.Result{Action: action, Service: service}
			}
		}(sub)
	}
	g.RUnlock()
}
//从registry.Result管道中读取消息
func (g *gossipRegistry) subscribe() (chan *registry.Result, chan bool) {
	next := make(chan *registry.Result, 10)
	exit := make(chan bool)

	id := uuid.New().String()

	g.Lock()
	g.watchers[id] = next
	g.Unlock()

	go func() {
		<-exit
		g.Lock()
		delete(g.watchers, id)
		close(next)
		g.Unlock()
	}()

	return next, exit
}

func (g *gossipRegistry) Stop() error {
	select {
	case <-g.done:
		return nil
	default:
		close(g.done)
		g.Lock()
		if g.member != nil {
			//广播离开的消息，但是后台监听不会关闭，仍然参与网络节点更新，直到离开的消息全部传达完毕或者超时退出
			g.member.Leave(g.interval * 2)
			//清除全部后台监听
			g.member.Shutdown()
			g.member = nil
		}
		g.Unlock()
	}
	return nil
}

// connectLoop attempts to reconnect to the memberlist
//每隔1秒检查memberlist事件，搜集其nodeActionLeave事件，如果有服务断开与cluster连接，将其重连至cluster
func (g *gossipRegistry) connectLoop() {
	// try every second
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-g.done:
			return
		case <-g.options.Context.Done():
			g.Stop()
			return
		case <-ticker.C:
			var addrs []string

			g.RLock()

			// only process if we have a memberlist
			if g.member == nil {
				g.RUnlock()
				continue
			}

			// self
			local := g.member.LocalNode().Address()

			// operate on each member
			for node, action := range g.members {
				switch action {
				// process leave event
				case nodeActionLeave:
					// don't process self
					if node == local {
						continue
					}
					addrs = append(addrs, node)
				}
			}

			g.RUnlock()

			// connect to all the members
			// TODO: only connect to new members
			if len(addrs) > 0 {
				g.connect(addrs)
			}
		}
	}
}

func (g *gossipRegistry) expiryLoop(updates *updates) {
	ticker := time.NewTicker(ExpiryTick)
	defer ticker.Stop()

	g.RLock()
	done := g.done
	g.RUnlock()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			now := uint64(time.Now().UnixNano())

			updates.Lock()

			// process all the updates
			for k, v := range updates.services {
				// check if expiry time has passed
				if d := (v.Update.Expires); d < now {
					// delete from records
					delete(updates.services, k)
					// set to delete
					v.Update.Action = actionTypeDelete
					// fire a new update
					g.updates <- v
				}
			}

			updates.Unlock()
		}
	}
}

// process member events
//处理memberlist对应的事件
func (g *gossipRegistry) eventLoop() {
	g.RLock()
	done := g.done
	g.RUnlock()
	for {
		select {
		// return when done
		case <-done:
			return
			//解析事件对应的服务，并将其添加至memberlist列表，等待connectLoop处理重连事件
		case ev := <-g.events:
			// TODO: nonblocking update
			g.Lock()
			if _, ok := g.members[ev.node]; ok {
				g.members[ev.node] = ev.action
			}
			g.Unlock()
		}
	}
}

func (g *gossipRegistry) run() {
	updates := &updates{
		services: make(map[uint64]*update),
	}

	// expiry loop
	//服务生命周期检测，如果超出生存周期，则删除服务
	go g.expiryLoop(updates)

	// event loop
	//处理event，解析event所属member，并将其添加到对应服务
	go g.eventLoop()

	g.RLock()
	// connect loop
	//如果设置重连，则将离线的服务重新连接至cluster
	if g.connectRetry {
		go g.connectLoop()
	}
	g.RUnlock()

	// process the updates
	//处理update服务
	for u := range g.updates {
		switch u.Update.Action {
		//服务新建，如果之前没有，则直接创建；否则融合服务
		case actionTypeCreate:
			g.Lock()
			if service, ok := g.services[u.Service.Name]; !ok {
				g.services[u.Service.Name] = []*registry.Service{u.Service}

			} else {
				g.services[u.Service.Name] = registry.Merge(service, []*registry.Service{u.Service})
			}
			g.Unlock()

			// publish update to watchers
			//发布消息，实则将消息写入watch的registry.Result管道，另外一端是subscribe
			//将update消息发送至watch的registry.Result管道，通知cache更新
			go g.publish(actionTypeString(actionTypeCreate), []*registry.Service{u.Service})

			// we need to expire the node at some point in the future
			//如果Expires参数生效，则将其添加至expiryLoop检查中，定期检查服务是否失效，失效则删除
			if u.Update.Expires > 0 {
				// create a hash of this service
				if hash, err := hashstructure.Hash(u.Service, nil); err == nil {
					updates.Lock()
					updates.services[hash] = u
					updates.Unlock()
				}
			}
		case actionTypeDelete:
			g.Lock()
			//删除服务，如果服务事先存在，返回删除后剩余节点；如果剩余节点为空，删除整个服务；否则将剩余节点放回即可
			if service, ok := g.services[u.Service.Name]; ok {
				if services := registry.Remove(service, []*registry.Service{u.Service}); len(services) == 0 {
					delete(g.services, u.Service.Name)
				} else {
					g.services[u.Service.Name] = services
				}
			}
			g.Unlock()

			// publish update to watchers
			//发布消息，实则将消息写入watch的registry.Result管道，另外一端是subscribe
			go g.publish(actionTypeString(actionTypeDelete), []*registry.Service{u.Service})

			// delete from expiry checks
			//从expiryLoop检查中删除对应服务
			if hash, err := hashstructure.Hash(u.Service, nil); err == nil {
				updates.Lock()
				delete(updates.services, hash)
				updates.Unlock()
			}
		case actionTypeSync:
			// no sync channel provided
			if u.sync == nil {
				continue
			}

			g.RLock()

			// push all services through the sync chan
			//通过sync管道同步所有服务
			for _, service := range g.services {
				for _, srv := range service {
					u.sync <- srv
				}

				// publish to watchers
				//发布消息，实则将消息写入watch的registry.Result管道，另外一端是subscribe
				go g.publish(actionTypeString(actionTypeCreate), service)
			}

			g.RUnlock()

			// close the sync chan
			close(u.sync)
		}
	}
}

func (g *gossipRegistry) Init(opts ...registry.Option) error {
	return configure(g, opts...)
}

func (g *gossipRegistry) Options() registry.Options {
	return g.options
}

func (g *gossipRegistry) Register(s *registry.Service, opts ...registry.RegisterOption) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}

	g.Lock()
	//如果本地services Map中不存在服务，新建
	if service, ok := g.services[s.Name]; !ok {
		g.services[s.Name] = []*registry.Service{s}
	} else {
		//否则与已有的服务合并
		g.services[s.Name] = registry.Merge(service, []*registry.Service{s})
	}
	g.Unlock()

	var options registry.RegisterOptions
	for _, o := range opts {
		o(&options)
	}

	if options.TTL == 0 && g.tcpInterval == 0 {
		return fmt.Errorf("Require register TTL or interval for memberlist.Config")
	}

	up := &pb.Update{
		Expires: uint64(time.Now().Add(options.TTL).UnixNano()),
		Action:  actionTypeCreate,
		Type:    updateTypeService,
		Metadata: map[string]string{
			"Content-Type": "application/json",
		},
		Data: b,
	}
	//向cluster内广播一个update数据包
	g.queue.QueueBroadcast(&broadcast{
		update: up,
		notify: nil,
	})

	// send update to local watchers
	//同时向本地watch发送同步请求
	g.updates <- &update{
		Update:  up,
		Service: s,
	}

	// wait
	<-time.After(g.interval * 2)

	return nil
}

func (g *gossipRegistry) Deregister(s *registry.Service) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}

	g.Lock()
	//如果map中存在该服务
	if service, ok := g.services[s.Name]; ok {
		//执行移除node操作，返回需要保留的node，如果没有需要保留的node，删除该服务
		if services := registry.Remove(service, []*registry.Service{s}); len(services) == 0 {
			delete(g.services, s.Name)
		} else {
			//否则将需要保留的node放回服务内
			g.services[s.Name] = services
		}
	}
	g.Unlock()
	up := &pb.Update{
		Action: actionTypeDelete,
		Type:   updateTypeService,
		Metadata: map[string]string{
			"Content-Type": "application/json",
		},
		Data: b,
	}
	//向cluster内广播一个update数据包
	g.queue.QueueBroadcast(&broadcast{
		update: up,
		notify: nil,
	})

	// send update to local watchers
	//同时向本地watch发送同步请求
	g.updates <- &update{
		Update:  up,
		Service: s,
	}

	// wait
	<-time.After(g.interval * 2)

	return nil
}

func (g *gossipRegistry) GetService(name string) ([]*registry.Service, error) {
	g.RLock()
	service, ok := g.services[name]
	g.RUnlock()
	if !ok {
		return nil, registry.ErrNotFound
	}
	return service, nil
}

func (g *gossipRegistry) ListServices() ([]*registry.Service, error) {
	var services []*registry.Service
	g.RLock()
	for _, service := range g.services {
		services = append(services, service...)
	}
	g.RUnlock()
	return services, nil
}

func (g *gossipRegistry) Watch(opts ...registry.WatchOption) (registry.Watcher, error) {
	//新建一个registry.Result管道，作为消息subscribe通道
	n, e := g.subscribe()
	//将该registry.Result管道作为参数，生成一个新的gossipWatcher
	return newGossipWatcher(n, e, opts...)
}

func (g *gossipRegistry) String() string {
	return "gossip"
}

func NewRegistry(opts ...registry.Option) registry.Registry {
	g := &gossipRegistry{
		options: registry.Options{
			Context: context.Background(),
		},
		done:     make(chan bool),
		events:   make(chan *event, 100),
		updates:  make(chan *update, 100),
		services: make(map[string][]*registry.Service),
		watchers: make(map[string]chan *registry.Result),
		members:  make(map[string]int32),
	}
	// run the updater
	//启动服务更新监听
	go g.run()

	// configure the gossiper
	if err := configure(g, opts...); err != nil {
		log.Fatalf("[gossip] Error configuring registry: %v", err)
	}
	// wait for setup
	<-time.After(g.interval * 2)

	return g
}
