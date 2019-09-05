package mdns

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// ServiceEntry is returned after we query for a service
type ServiceEntry struct {
	Name       string
	Host       string
	AddrV4     net.IP
	AddrV6     net.IP
	Port       int
	Info       string
	InfoFields []string
	TTL        int

	Addr net.IP // @Deprecated

	hasTXT bool
	sent   bool
}

// complete is used to check if we have all the info we need
//如果同时获取地址，port以及txt信息，说明服务完整返回
func (s *ServiceEntry) complete() bool {
	return (s.AddrV4 != nil || s.AddrV6 != nil || s.Addr != nil) && s.Port != 0 && s.hasTXT
}

// QueryParam is used to customize how a Lookup is performed
type QueryParam struct {
	Service             string               // Service to lookup
	Domain              string               // Lookup domain, default "local"
	Context             context.Context      // Context
	Timeout             time.Duration        // Lookup timeout, default 1 second. Ignored if Context is provided
	Interface           *net.Interface       // Multicast interface to use
	Entries             chan<- *ServiceEntry // Entries Channel
	WantUnicastResponse bool                 // Unicast response desired, as per 5.4 in RFC
}

// DefaultParams is used to return a default set of QueryParam's
func DefaultParams(service string) *QueryParam {
	return &QueryParam{
		Service:             service,
		Domain:              "local",
		Timeout:             time.Second,
		Entries:             make(chan *ServiceEntry),
		WantUnicastResponse: false, // TODO(reddaly): Change this default.
	}
}

// Query looks up a given service, in a domain, waiting at most
// for a timeout before finishing the query. The results are streamed
// to a channel. Sends will not block, so clients should make sure to
// either read or buffer.
func Query(params *QueryParam) error {
	// Create a new client
	//创建Client，实际上是新建一个udp广播句柄
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	// Set the multicast interface
	//设置广播绑定的interface
	if params.Interface != nil {
		if err := client.setInterface(params.Interface, false); err != nil {
			return err
		}
	}

	// Ensure defaults are set
	//设置主机域名，如果未指定，用local
	if params.Domain == "" {
		params.Domain = "local"
	}
	//设置Context参数，主要为超时参数
	if params.Context == nil {
		if params.Timeout == 0 {
			params.Timeout = time.Second
		}
		params.Context, _ = context.WithTimeout(context.Background(), params.Timeout)
		if err != nil {
			return err
		}
	}

	// Run the query
	return client.query(params)
}

// Listen listens indefinitely for multicast updates
func Listen(entries chan<- *ServiceEntry, exit chan struct{}) error {
	// Create a new client
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()

	client.setInterface(nil, true)

	// Start listening for response packets
	msgCh := make(chan *dns.Msg, 32)

	go client.recv(client.ipv4MulticastConn, msgCh)
	go client.recv(client.ipv6MulticastConn, msgCh)
	go client.recv(client.ipv4MulticastConn, msgCh)
	go client.recv(client.ipv6MulticastConn, msgCh)

	ip := make(map[string]*ServiceEntry)

	for {
		select {
		case <-exit:
			return nil
		case <-client.closedCh:
			return nil
		case m := <-msgCh:
			//根据接收的消息，如果确认返回完整服务，则发送至管道，等待进一步处理
			e := messageToEntry(m, ip)
			if e == nil {
				continue
			}

			// Check if this entry is complete
			if e.complete() {
				if e.sent {
					continue
				}
				//如果获取完整服务，标记并发送至管道进一步处理
				e.sent = true
				entries <- e
				ip = make(map[string]*ServiceEntry)
			} else {
				// Fire off a node specific query
				//否则说明该服务没有成功返回，再请求一次
				m := new(dns.Msg)
				m.SetQuestion(e.Name, dns.TypePTR)
				m.RecursionDesired = false
				if err := client.sendQuery(m); err != nil {
					log.Printf("[ERR] mdns: Failed to query instance %s: %v", e.Name, err)
				}
			}
		}
	}

	return nil
}

// Lookup is the same as Query, however it uses all the default parameters
func Lookup(service string, entries chan<- *ServiceEntry) error {
	params := DefaultParams(service)
	params.Entries = entries
	return Query(params)
}

// Client provides a query interface that can be used to
// search for service providers using mDNS
type client struct {
	ipv4UnicastConn *net.UDPConn
	ipv6UnicastConn *net.UDPConn

	ipv4MulticastConn *net.UDPConn
	ipv6MulticastConn *net.UDPConn

	closed    bool
	closedCh  chan struct{} // TODO(reddaly): This doesn't appear to be used.
	closeLock sync.Mutex
}

// NewClient creates a new mdns Client that can be used to query
// for records
func newClient() (*client, error) {
	// TODO(reddaly): At least attempt to bind to the port required in the spec.
	// Create a IPv4 listener
	uconn4, err4 := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	uconn6, err6 := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6zero, Port: 0})
	if err4 != nil && err6 != nil {
		log.Printf("[ERR] mdns: Failed to bind to udp port: %v %v", err4, err6)
	}

	if uconn4 == nil && uconn6 == nil {
		return nil, fmt.Errorf("failed to bind to any unicast udp port")
	}

	mconn4, err4 := net.ListenMulticastUDP("udp4", nil, ipv4Addr)
	mconn6, err6 := net.ListenMulticastUDP("udp6", nil, ipv6Addr)
	if err4 != nil && err6 != nil {
		log.Printf("[ERR] mdns: Failed to bind to multicast udp port: %v %v", err4, err6)
	}

	if mconn4 == nil && mconn6 == nil {
		return nil, fmt.Errorf("failed to bind to any multicast udp port")
	}

	c := &client{
		ipv4MulticastConn: mconn4,
		ipv6MulticastConn: mconn6,
		ipv4UnicastConn:   uconn4,
		ipv6UnicastConn:   uconn6,
		closedCh:          make(chan struct{}),
	}
	return c, nil
}

// Close is used to cleanup the client
func (c *client) Close() error {
	c.closeLock.Lock()
	defer c.closeLock.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	close(c.closedCh)

	if c.ipv4UnicastConn != nil {
		c.ipv4UnicastConn.Close()
	}
	if c.ipv6UnicastConn != nil {
		c.ipv6UnicastConn.Close()
	}
	if c.ipv4MulticastConn != nil {
		c.ipv4MulticastConn.Close()
	}
	if c.ipv6MulticastConn != nil {
		c.ipv6MulticastConn.Close()
	}

	return nil
}

// setInterface is used to set the query interface, uses sytem
// default if not provided
func (c *client) setInterface(iface *net.Interface, loopback bool) error {
	p := ipv4.NewPacketConn(c.ipv4UnicastConn)
	if err := p.JoinGroup(iface, &net.UDPAddr{IP: mdnsGroupIPv4}); err != nil {
		return err
	}
	p2 := ipv6.NewPacketConn(c.ipv6UnicastConn)
	if err := p2.JoinGroup(iface, &net.UDPAddr{IP: mdnsGroupIPv6}); err != nil {
		return err
	}
	p = ipv4.NewPacketConn(c.ipv4MulticastConn)
	if err := p.JoinGroup(iface, &net.UDPAddr{IP: mdnsGroupIPv4}); err != nil {
		return err
	}
	p2 = ipv6.NewPacketConn(c.ipv6MulticastConn)
	if err := p2.JoinGroup(iface, &net.UDPAddr{IP: mdnsGroupIPv6}); err != nil {
		return err
	}

	if loopback {
		p.SetMulticastLoopback(true)
		p2.SetMulticastLoopback(true)
	}

	return nil
}

// query is used to perform a lookup and stream results
//发送请求
func (c *client) query(params *QueryParam) error {
	// Create the service name
	serviceAddr := fmt.Sprintf("%s.%s.", trimDot(params.Service), trimDot(params.Domain))

	// Start listening for response packets
	//先设置监听
	msgCh := make(chan *dns.Msg, 32)
	//在接收线程中传入msg管道，用于将接收的消息传递出来，等待处理
	go c.recv(c.ipv4UnicastConn, msgCh)
	go c.recv(c.ipv6UnicastConn, msgCh)
	go c.recv(c.ipv4MulticastConn, msgCh)
	go c.recv(c.ipv6MulticastConn, msgCh)

	// Send the query
	m := new(dns.Msg)
	//设置请求参数
	m.SetQuestion(serviceAddr, dns.TypePTR)
	// RFC 6762, section 18.12.  Repurposing of Top Bit of qclass in Question
	// Section
	//
	// In the Question Section of a Multicast DNS query, the top bit of the qclass
	// field is used to indicate that unicast responses are preferred for this
	// particular question.  (See Section 5.4.)
	//如果设置了单播自动返回，则设置对应参数
	if params.WantUnicastResponse {
		m.Question[0].Qclass |= 1 << 15
	}
	m.RecursionDesired = false
	//发送请求
	if err := c.sendQuery(m); err != nil {
		return err
	}

	// Map the in-progress responses
	inprogress := make(map[string]*ServiceEntry)
	//等待消息回复
	for {
		select {
		//从接收函数的管道中获取返回数据
		case resp := <-msgCh:
			//将收到的message转换成ServiceEntry,主要解析PTR，SRV，TXT
			inp := messageToEntry(resp, inprogress)
			if inp == nil {
				continue
			}

			// Check if this entry is complete
			//检查返回是否完整
			if inp.complete() {
				if inp.sent {
					continue
				}
				inp.sent = true
				select {
				//如果接收完整，则将服务发送至管道等待处理
				case params.Entries <- inp:
				case <-params.Context.Done():
					return nil
				}
			} else {
				//如果不完整，则针对该node，再次发送请求
				// Fire off a node specific query
				m := new(dns.Msg)
				m.SetQuestion(inp.Name, dns.TypePTR)
				m.RecursionDesired = false
				if err := c.sendQuery(m); err != nil {
					log.Printf("[ERR] mdns: Failed to query instance %s: %v", inp.Name, err)
				}
			}
		case <-params.Context.Done():
			return nil
		}
	}
}

// sendQuery is used to multicast a query out
func (c *client) sendQuery(q *dns.Msg) error {
	buf, err := q.Pack()
	if err != nil {
		return err
	}
	if c.ipv4UnicastConn != nil {
		c.ipv4UnicastConn.WriteToUDP(buf, ipv4Addr)
	}
	if c.ipv6UnicastConn != nil {
		c.ipv6UnicastConn.WriteToUDP(buf, ipv6Addr)
	}
	return nil
}

// recv is used to receive until we get a shutdown
func (c *client) recv(l *net.UDPConn, msgCh chan *dns.Msg) {
	if l == nil {
		return
	}
	buf := make([]byte, 65536)
	for {
		c.closeLock.Lock()
		if c.closed {
			c.closeLock.Unlock()
			return
		}
		c.closeLock.Unlock()
		n, err := l.Read(buf)
		if err != nil {
			continue
		}
		msg := new(dns.Msg)
		if err := msg.Unpack(buf[:n]); err != nil {
			continue
		}
		select {
		case msgCh <- msg:
		case <-c.closedCh:
			return
		}
	}
}

// ensureName is used to ensure the named node is in progress
//确认该服务是否正在被处理，如果是，直接返回；如果不是，添加至列表
func ensureName(inprogress map[string]*ServiceEntry, name string) *ServiceEntry {
	if inp, ok := inprogress[name]; ok {
		return inp
	}
	inp := &ServiceEntry{
		Name: name,
	}
	inprogress[name] = inp
	return inp
}

// alias is used to setup an alias between two entries
func alias(inprogress map[string]*ServiceEntry, src, dst string) {
	srcEntry := ensureName(inprogress, src)
	inprogress[dst] = srcEntry
}
//获取完整的ServiceEntry，包括addr，port以及txt信息
func messageToEntry(m *dns.Msg, inprogress map[string]*ServiceEntry) *ServiceEntry {
	var inp *ServiceEntry

	for _, answer := range append(m.Answer, m.Extra...) {
		// TODO(reddaly): Check that response corresponds to serviceAddr?
		switch rr := answer.(type) {
		//如果返回PTR，则获得实例名称
		case *dns.PTR:
			// Create new entry for this
			inp = ensureName(inprogress, rr.Ptr)
			if inp.complete() {
				continue
			}
			//如果返回SRV，解析host和ip信息
		case *dns.SRV:
			// Check for a target mismatch
			if rr.Target != rr.Hdr.Name {
				alias(inprogress, rr.Hdr.Name, rr.Target)
			}

			// Get the port
			inp = ensureName(inprogress, rr.Hdr.Name)
			if inp.complete() {
				continue
			}
			inp.Host = rr.Target
			inp.Port = int(rr.Port)
			//如果是TXT，解析服务具体信息，并标记hasTXT参数
		case *dns.TXT:
			// Pull out the txt
			inp = ensureName(inprogress, rr.Hdr.Name)
			if inp.complete() {
				continue
			}
			inp.Info = strings.Join(rr.Txt, "|")
			inp.InfoFields = rr.Txt
			inp.hasTXT = true
			//对方返回的IPV4地址，通过A Record返回
		case *dns.A:
			// Pull out the IP
			inp = ensureName(inprogress, rr.Hdr.Name)
			if inp.complete() {
				continue
			}
			inp.Addr = rr.A // @Deprecated
			inp.AddrV4 = rr.A
			//对方返回的IPV6地址，通过AAAA Record
		case *dns.AAAA:
			// Pull out the IP
			inp = ensureName(inprogress, rr.Hdr.Name)
			if inp.complete() {
				continue
			}
			inp.Addr = rr.AAAA // @Deprecated
			inp.AddrV6 = rr.AAAA
		}

		if inp != nil {
			inp.TTL = int(answer.Header().Ttl)
		}
	}

	return inp
}
