package memory

import (
	"errors"

	"github.com/micro/go-micro/registry"
)

type Watcher struct {
	id   string
	wo   registry.WatchOptions
	res  chan *registry.Result
	exit chan bool
}

func (m *Watcher) Next() (*registry.Result, error) {
	for {
		select {
		case r := <-m.res:
			//如果在WatchOptions中指定了要watch的service name，则查看获取的服务是否与指定的服务相同;相同返回，不相同继续
			if len(m.wo.Service) > 0 && m.wo.Service != r.Service.Name {
				continue
			}
			//如果没有指定要watch的service name，则watch所有服务，找到即返回
			return r, nil
		case <-m.exit:
			return nil, errors.New("watcher stopped")
		}
	}
}

func (m *Watcher) Stop() {
	select {
	case <-m.exit:
		return
	default:
		close(m.exit)
	}
}
