package learn

//go micro中Broker解析

/*
type Broker interface {
    Options() Options
    Address() string
    Connect() error ///启动broker服务
    Disconnect() error ///关闭Broker服务
    Init(...Option) error
    Publish(string, *Message, ...PublishOption) error  ///publish topic message
    Subscribe(string, Handler, ...SubscribeOption) (Subscriber, error)  ///注册 topic message 的 subscribe
    String() string
}
*/

/*
订阅服务就是注册一个Topic serivce 到 Consul,并且在这个端口（topic）进行监听了，等待消息接收
发布消息的整个逻辑也非常简单：
获取对应topic的server
编码对应的消息
按照service的类型把消息通过http post的方式发送出去【异步发送】
*/

/*
func (h *httpBroker) Subscribe(topic string, handler Handler, opts ...SubscribeOption) (Subscriber, error) {
	.....
	// register service
	/// 当注册一个subscriber的时候实际上注册了一个服务。然后publish通过服务的名称找到这个注册的地址，然后发送消息。
	node := &registry.Node{
		Id:      id,
		Address: addr,
		Port:    port,
		Metadata: map[string]string{
			"secure": fmt.Sprintf("%t", secure),
		},
	}

	// check for queue group or broadcast queue
	version := options.Queue
	if len(version) == 0 {
		version = broadcastVersion
	}

	service := &registry.Service{
		Name:    "topic:" + topic,
		Version: version,
		Nodes:   []*registry.Node{node},
	}

	// generate subscriber
	subscriber := &httpSubscriber{
		opts:  options,
		hb:    h,
		id:    id,
		topic: topic,
		fn:    handler,///等收到publish是的回调。
		svc:   service,
	}

	// subscribe now
	////注册服务。并且把subscribe append 到 httpBroker.subscribers中
	if err := h.subscribe(subscriber); err != nil {
		return nil, err
	}

	// return the subscriber
	return subscriber, nil
}

func (h *httpBroker) subscribe(s *httpSubscriber) error {
	h.Lock()
	defer h.Unlock()

	if err := h.r.Register(s.svc, registry.RegisterTTL(registerTTL)); err != nil {
		return err
	}
	h.subscribers[s.topic] = append(h.subscribers[s.topic], s)
	return nil
}

func (h *httpBroker) Publish(topic string, msg *Message, opts ...PublishOption) error {
    h.RLock()
    s, err := h.r.GetService("topic:" + topic)///发现相关服务
    if err != nil {
        h.RUnlock()
        return err
    }
    h.RUnlock()

    m := &Message{
        Header: make(map[string]string),
        Body:   msg.Body,
    }

    for k, v := range msg.Header {
        m.Header[k] = v
    }

    m.Header[":topic"] = topic

    b, err := h.opts.Codec.Marshal(m)///对消息进行编码
    if err != nil {
        return err
    }

    pub := func(node *registry.Node, b []byte) {
        scheme := "http"

        // check if secure is added in metadata
        if node.Metadata["secure"] == "true" {
            scheme = "https"
        }

        vals := url.Values{}
        vals.Add("id", node.Id)

        uri := fmt.Sprintf("%s://%s:%d%s?%s", scheme, node.Address, node.Port, DefaultSubPath, vals.Encode())
        r, err := h.c.Post(uri, "application/json", bytes.NewReader(b))
        if err == nil {
            io.Copy(ioutil.Discard, r.Body)
            r.Body.Close()
        }
    }

    for _, service := range s {
        // only process if we have nodes
        if len(service.Nodes) == 0 {
            continue
        }

        switch service.Version {
        // broadcast version means broadcast to all nodes
        case broadcastVersion:///广播
            for _, node := range service.Nodes {
                // publish async
                go pub(node, b)
            }
        default:
            // select node to publish to///随机publish一个service
            node := service.Nodes[rand.Int()%len(service.Nodes)]

            // publish async
            go pub(node, b)
        }
    }

    return nil
}
*/
