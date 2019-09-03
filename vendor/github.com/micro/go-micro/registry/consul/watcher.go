package consul

import (
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/api/watch"
	"github.com/micro/go-micro/registry"
)

type consulWatcher struct {
	r        *consulRegistry
	wo       registry.WatchOptions
	wp       *watch.Plan
	watchers map[string]*watch.Plan

	next chan *registry.Result
	exit chan bool

	sync.RWMutex
	services map[string][]*registry.Service
}

func newConsulWatcher(cr *consulRegistry, opts ...registry.WatchOption) (registry.Watcher, error) {
	var wo registry.WatchOptions
	for _, o := range opts {
		o(&wo)
	}

	cw := &consulWatcher{
		r:        cr,
		wo:       wo,
		exit:     make(chan bool),
		next:     make(chan *registry.Result, 10),
		watchers: make(map[string]*watch.Plan),
		services: make(map[string][]*registry.Service),
	}
	//发起一个watch请求，返回watch plan
	wp, err := watch.Parse(map[string]interface{}{"type": "services"})
	if err != nil {
		return nil, err
	}

	wp.Handler = cw.handle
	go wp.RunWithClientAndLogger(cr.Client, log.New(os.Stderr, "", log.LstdFlags))
	cw.wp = wp

	return cw, nil
}
//更新consulWatcher中的服务
//对比老服务consulWatcher与新服务data，如果有新的服务加入，则创建
//如果服务存在，但是需要更新节点，则更新
//如果新服务中删除了部分老服务，则删除
func (cw *consulWatcher) serviceHandler(idx uint64, data interface{}) {
	//类型强转
	entries, ok := data.([]*api.ServiceEntry)
	if !ok {
		return
	}

	serviceMap := map[string]*registry.Service{}
	serviceName := ""
	//解析最新传入的参数
	for _, e := range entries {
		//解析service各项参数
		serviceName = e.Service.Service
		// version is now a tag
		version, _ := decodeVersion(e.Service.Tags)
		// service ID is now the node id
		id := e.Service.ID
		// key is always the version
		key := version
		// address is service address
		address := e.Service.Address

		// use node address
		if len(address) == 0 {
			address = e.Node.Address
		}
		//查找map中是否已经存在service，不存在则添加
		svc, ok := serviceMap[key]
		if !ok {
			svc = &registry.Service{
				Endpoints: decodeEndpoints(e.Service.Tags),
				Name:      e.Service.Service,
				Version:   version,
			}
			serviceMap[key] = svc
		}

		var del bool
		//如果服务状态为不健康，则直接跳过，即不添加
		for _, check := range e.Checks {
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
		//服务健康，则向服务中添加节点信息
		svc.Nodes = append(svc.Nodes, &registry.Node{
			Id:       id,
			Address:  fmt.Sprintf("%s:%d", address, e.Service.Port),
			Metadata: decodeMetadata(e.Service.Tags),
		})
	}

	cw.RLock()
	// make a copy
	//备份consulWatcher的老的service
	rservices := make(map[string][]*registry.Service)
	for k, v := range cw.services {
		rservices[k] = v
	}
	cw.RUnlock()

	var newServices []*registry.Service

	// serviceMap is the new set of services keyed by name+version
	//遍历最新服务，查看是否需要在老的consulWatcher中新建服务或者更新服务
	for _, newService := range serviceMap {
		// append to the new set of cached services
		newServices = append(newServices, newService)

		// check if the service exists in the existing cache
		//查看服务在老的consulWatcher中是否存在，不存在则创建
		oldServices, ok := rservices[serviceName]
		if !ok {
			// does not exist? then we're creating brand new entries
			cw.next <- &registry.Result{Action: "create", Service: newService}
			continue
		}

		// service exists. ok let's figure out what to update and delete version wise
		action := "create"
		//如果老的consulWatcher中已存在该服务
		for _, oldService := range oldServices {
			// does this version exist?
			// no? then default to create
			//如果版本不相同，则重新创建
			if oldService.Version != newService.Version {
				continue
			}

			// yes? then it's an update
			//否则更新即可
			action = "update"

			var nodes []*registry.Node
			// check the old nodes to see if they've been deleted
			//对于老的consulWatcher中已存在的服务，并且version相同，先对比两个版本的Nodes，
			//查看oldService是否有newService不再使用的Node，如果有，
			//则单独拎出来，做删除处理
			for _, oldNode := range oldService.Nodes {
				var seen bool
				for _, newNode := range newService.Nodes {
					if newNode.Id == oldNode.Id {
						seen = true
						break
					}
				}
				// does the old node exist in the new set of nodes
				// no? then delete that shit
				//拎出newService中不存在的节点(说明不再使用)，一并删除
				if !seen {
					nodes = append(nodes, oldNode)
				}
			}

			// it's an update rather than creation
			if len(nodes) > 0 {
				delService := oldService
				delService.Nodes = nodes
				cw.next <- &registry.Result{Action: "delete", Service: delService}
			}
		}
		//执行对应的操作，create或者update
		cw.next <- &registry.Result{Action: action, Service: newService}
	}

	// Now check old versions that may not be in new services map
	//老的consulWatcher服务是否存在新服务不再使用的服务，如果存在，则删除
	for _, old := range rservices[serviceName] {
		// old version does not exist in new version map
		// kill it with fire!
		if _, ok := serviceMap[old.Version]; !ok {
			cw.next <- &registry.Result{Action: "delete", Service: old}
		}
	}

	cw.Lock()
	//更新consulWatcher中的服务为最新的
	cw.services[serviceName] = newServices
	cw.Unlock()
}
//根据传入的新服务，更新consulWatcher内容
//如果有新的服务，添加
//如果老服务不再使用，删除consulWatcher中对应的watchers和services
func (cw *consulWatcher) handle(idx uint64, data interface{}) {
	//获取需要watch的新服务
	services, ok := data.(map[string][]string)
	if !ok {
		return
	}

	// add new watchers
	//遍历新服务，查看那些在老服务中不存在，不存在则添加
	for service, _ := range services {
		// Filter on watch options
		// wo.Service: Only watch services we care about
		//如果watch option指定了要监听的服务，则只监听对应服务，否则监听所有服务
		if len(cw.wo.Service) > 0 && service != cw.wo.Service {
			continue
		}
		//如果该服务已经被监听，则不需要再次加入
		if _, ok := cw.watchers[service]; ok {
			continue
		}
		//加入服务到监听列表
		//创建watch plan
		wp, err := watch.Parse(map[string]interface{}{
			"type":    "service",
			"service": service,
		})
		//添加新服务到consulWatcher，并为服务添加watch plan以及handler，同步到cache
		if err == nil {
			//更新监听服务watch plan的handler
			wp.Handler = cw.serviceHandler
			go wp.RunWithClientAndLogger(cw.r.Client, log.New(os.Stderr, "", log.LstdFlags))
			//加入到consulWatcher
			cw.watchers[service] = wp
			//新建一个create registry.Result命令到管道，异步处理命令
			cw.next <- &registry.Result{Action: "create", Service: &registry.Service{Name: service}}
		}
	}

	cw.RLock()
	// make a copy
	//对consulWatcher中老的services进行备份
	rservices := make(map[string][]*registry.Service)
	for k, v := range cw.services {
		rservices[k] = v
	}
	cw.RUnlock()

	// remove unknown services from registry
	// save the things we want to delete
	//用于暂存被删除的服务
	deleted := make(map[string][]*registry.Service)
	//遍历老服务，查看哪些在新服务中已经不再使用，则删除，删除后的service暂存于deleted中
	//删除cw.services
	for service, _ := range rservices {
		if _, ok := services[service]; !ok {
			cw.Lock()
			// save this before deleting
			deleted[service] = cw.services[service]
			//从老服务consulWatcher中删除不再使用的
			delete(cw.services, service)
			cw.Unlock()
		}
	}

	// remove unknown services from watchers
	//遍历老的consulWatcher，查看哪些在新服务中已经不再使用，则删除
	//删除cw.watchers，并异步删除cache内容
	for service, w := range cw.watchers {
		if _, ok := services[service]; !ok {
			//停止服务监听
			w.Stop()
			//从watch map中删除
			delete(cw.watchers, service)
			//顺便删除老服务中对应的service服务
			for _, oldService := range deleted[service] {
				// send a delete for the service nodes that we're removing
				cw.next <- &registry.Result{Action: "delete", Service: oldService}
			}
			// sent the empty list as the last resort to indicate to delete the entire service
			//删除空节点服务
			cw.next <- &registry.Result{Action: "delete", Service: &registry.Service{Name: service}}
		}
	}
}

func (cw *consulWatcher) Next() (*registry.Result, error) {
	select {
	case <-cw.exit:
		return nil, registry.ErrWatcherStopped
	case r, ok := <-cw.next:
		if !ok {
			return nil, registry.ErrWatcherStopped
		}
		return r, nil
	}
	// NOTE: This is a dead code path: e.g. it will never be reached
	// as we return in all previous code paths never leading to this return
	return nil, registry.ErrWatcherStopped
}

func (cw *consulWatcher) Stop() {
	select {
	case <-cw.exit:
		return
	default:
		close(cw.exit)
		if cw.wp == nil {
			return
		}
		cw.wp.Stop()

		// drain results
		for {
			select {
			case <-cw.next:
			default:
				return
			}
		}
	}
}
