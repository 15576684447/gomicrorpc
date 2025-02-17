package registry

func addNodes(old, neu []*Node) []*Node {
	var nodes []*Node

	// add all new nodes
	for _, n := range neu {
		node := *n
		nodes = append(nodes, &node)
	}

	// look at old nodes
	for _, o := range old {
		var exists bool

		// check against new nodes
		for _, n := range nodes {
			// ids match then skip
			if o.Id == n.Id {
				exists = true
				break
			}
		}

		// keep old node
		if !exists {
			node := *o
			nodes = append(nodes, &node)
		}
	}

	return nodes
}

func delNodes(old, del []*Node) []*Node {
	var nodes []*Node
	for _, o := range old {
		var rem bool
		for _, n := range del {
			if o.Id == n.Id {
				rem = true
				break
			}
		}
		if !rem {
			nodes = append(nodes, o)
		}
	}
	return nodes
}

// Copy makes a copy of services
func Copy(current []*Service) []*Service {
	var services []*Service

	for _, service := range current {
		// copy service
		s := new(Service)
		*s = *service

		// copy nodes
		var nodes []*Node
		for _, node := range service.Nodes {
			n := new(Node)
			*n = *node
			nodes = append(nodes, n)
		}
		s.Nodes = nodes

		// copy endpoints
		var eps []*Endpoint
		for _, ep := range service.Endpoints {
			e := new(Endpoint)
			*e = *ep
			eps = append(eps, e)
		}
		s.Endpoints = eps

		// append service
		services = append(services, s)
	}

	return services
}

// Merge merges two lists of services and returns a new copy
//将两个Service融合为一个Service
func Merge(olist []*Service, nlist []*Service) []*Service {
	var srv []*Service
	//融合规则:
	//遍历new list，并将每个new list与所有old list对比
	//如果Version相同，则在old Service的copy版本中添加new Service的Nodes(只融合Nodes)，然后添加到srv
	//如果Version不相同，将old Service与new Service同时添加到srv
	for _, n := range nlist {
		var seen bool
		for _, o := range olist {
			if o.Version == n.Version {
				sp := new(Service)
				// make copy
				*sp = *o
				// set nodes
				sp.Nodes = addNodes(o.Nodes, n.Nodes)

				// mark as seen
				seen = true
				srv = append(srv, sp)
				break
			} else {
				sp := new(Service)
				// make copy
				*sp = *o
				srv = append(srv, sp)
			}
		}
		if !seen {
			srv = append(srv, Copy([]*Service{n})...)
		}
	}
	return srv
}

// Remove removes services and returns a new copy
//删除Service节点: 遍历old节点，如果Version与待删除的节点不同，重新放回; 否则舍弃
func Remove(old, del []*Service) []*Service {
	var services []*Service

	for _, o := range old {
		srv := new(Service)
		*srv = *o

		var rem bool

		for _, s := range del {
			if srv.Version == s.Version {
				srv.Nodes = delNodes(srv.Nodes, s.Nodes)

				if len(srv.Nodes) == 0 {
					rem = true
				}
			}
		}

		if !rem {
			services = append(services, srv)
		}
	}

	return services
}
