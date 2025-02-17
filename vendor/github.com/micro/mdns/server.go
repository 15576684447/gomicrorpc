package mdns

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)
//mdns预留的IPV4与IPV6地址以及其端口号
//包括单播与广播地址
var (
	mdnsGroupIPv4 = net.ParseIP("224.0.0.251")
	mdnsGroupIPv6 = net.ParseIP("ff02::fb")

	// mDNS wildcard addresses
	//组内广播地址
	mdnsWildcardAddrIPv4 = &net.UDPAddr{
		IP:   net.ParseIP("224.0.0.0"),
		Port: 5353,
	}
	mdnsWildcardAddrIPv6 = &net.UDPAddr{
		IP:   net.ParseIP("ff02::"),
		Port: 5353,
	}

	// mDNS endpoint addresses
	//单播地址
	ipv4Addr = &net.UDPAddr{
		IP:   mdnsGroupIPv4,
		Port: 5353,
	}
	ipv6Addr = &net.UDPAddr{
		IP:   mdnsGroupIPv6,
		Port: 5353,
	}
)

// Config is used to configure the mDNS server
type Config struct {
	// Zone must be provided to support responding to queries
	Zone Zone

	// Iface if provided binds the multicast listener to the given
	// interface. If not provided, the system default multicase interface
	// is used.
	Iface *net.Interface
}

// mDNS server is used to listen for mDNS queries and respond if we
// have a matching local record
type Server struct {
	config *Config

	ipv4List *net.UDPConn
	ipv6List *net.UDPConn

	shutdown     bool
	shutdownCh   chan struct{}
	shutdownLock sync.Mutex
	wg           sync.WaitGroup
}

// NewServer is used to create a new mDNS server from a config
func NewServer(config *Config) (*Server, error) {
	// Create the listeners
	// Create wildcard connections (because :5353 can be already taken by other apps)
	//创建udp服务监听
	ipv4List, _ := net.ListenUDP("udp4", mdnsWildcardAddrIPv4)
	ipv6List, _ := net.ListenUDP("udp6", mdnsWildcardAddrIPv6)
	if ipv4List == nil && ipv6List == nil {
		return nil, fmt.Errorf("[ERR] mdns: Failed to bind to any udp port!")
	}

	if ipv4List == nil {
		ipv4List = &net.UDPConn{}
	}
	if ipv6List == nil {
		ipv6List = &net.UDPConn{}
	}

	// Join multicast groups to receive announcements
	//加入广播组接收消息
	p1 := ipv4.NewPacketConn(ipv4List)
	p2 := ipv6.NewPacketConn(ipv6List)
	//设置是否回传，如果是，则收到对方数据包后，将数据拷贝一份，回传给对方
	p1.SetMulticastLoopback(true)
	p2.SetMulticastLoopback(true)
	//如果设置了广播绑定的interface，则使用指定的interface，否则使用系统默认的interface
	if config.Iface != nil {
		//使用预设的interface绑定广播监听
		if err := p1.JoinGroup(config.Iface, &net.UDPAddr{IP: mdnsGroupIPv4}); err != nil {
			return nil, err
		}
		if err := p2.JoinGroup(config.Iface, &net.UDPAddr{IP: mdnsGroupIPv6}); err != nil {
			return nil, err
		}
	} else {
		//使用系统默认的Interface
		ifaces, err := net.Interfaces()
		if err != nil {
			return nil, err
		}
		//计数绑定失败个数
		errCount1, errCount2 := 0, 0
		//将广播绑定到系统默认的interface
		for _, iface := range ifaces {
			if err := p1.JoinGroup(&iface, &net.UDPAddr{IP: mdnsGroupIPv4}); err != nil {
				errCount1++
			}
			if err := p2.JoinGroup(&iface, &net.UDPAddr{IP: mdnsGroupIPv6}); err != nil {
				errCount2++
			}
		}
		//如果全部绑定失败
		if len(ifaces) == errCount1 && len(ifaces) == errCount2 {
			return nil, fmt.Errorf("Failed to join multicast group on all interfaces!")
		}
	}

	s := &Server{
		config:     config,
		ipv4List:   ipv4List,
		ipv6List:   ipv6List,
		shutdownCh: make(chan struct{}),
	}
	//启动服务监听线程
	go s.recv(s.ipv4List)
	go s.recv(s.ipv6List)
	//对象内部计数器，初始值为0，使用Add()可以设置初始值，执行Done()会使计数值减1，使用Wait()会阻塞等待，直到计数值为0
	//其作用是等待一个对象的所有实例全部释放完毕才结束
	s.wg.Add(1)
	//probe运行完毕后，会执行Down()函数
	//向组内广播自己的服务信息，以注册自己的服务，并设置监听，自动回复应答
	go s.probe()

	return s, nil
}

// Shutdown is used to shutdown the listener
func (s *Server) Shutdown() error {
	s.shutdownLock.Lock()
	defer s.shutdownLock.Unlock()

	if s.shutdown {
		return nil
	}

	s.shutdown = true
	close(s.shutdownCh)
	//广播发送注销消息
	s.unregister()

	if s.ipv4List != nil {
		s.ipv4List.Close()
	}
	if s.ipv6List != nil {
		s.ipv6List.Close()
	}
	//等待实例全部销毁完毕才正式退出
	s.wg.Wait()
	return nil
}

// recv is a long running routine to receive packets from an interface
//从网络接口接收局域网内广播消息
func (s *Server) recv(c *net.UDPConn) {
	if c == nil {
		return
	}
	buf := make([]byte, 65536)
	for {
		s.shutdownLock.Lock()
		if s.shutdown {
			s.shutdownLock.Unlock()
			return
		}
		s.shutdownLock.Unlock()
		//将读取的数据写入buf，返回实际获取的字节数以及对方地址
		n, from, err := c.ReadFrom(buf)
		if err != nil {
			continue
		}
		//解析获取的数据
		if err := s.parsePacket(buf[:n], from); err != nil {
			log.Printf("[ERR] mdns: Failed to handle query: %v", err)
		}
	}
}

// parsePacket is used to parse an incoming packet
func (s *Server) parsePacket(packet []byte, from net.Addr) error {
	var msg dns.Msg
	//解压缩数据
	if err := msg.Unpack(packet); err != nil {
		log.Printf("[ERR] mdns: Failed to unpack packet: %v", err)
		return err
	}
	// TODO: This is a bit of a hack
	// We decided to ignore some mDNS answers for the time being
	// See: https://tools.ietf.org/html/rfc6762#section-7.2
	msg.Truncated = false
	//处理请求
	return s.handleQuery(&msg, from)
}

// handleQuery is used to handle an incoming query
//处理请求
func (s *Server) handleQuery(query *dns.Msg, from net.Addr) error {
	//OPCODE必须为0
	if query.Opcode != dns.OpcodeQuery {
		// "In both multicast query and multicast response messages, the OPCODE MUST
		// be zero on transmission (only standard queries are currently supported
		// over multicast).  Multicast DNS messages received with an OPCODE other
		// than zero MUST be silently ignored."  Note: OpcodeQuery == 0
		return fmt.Errorf("mdns: received query with non-zero Opcode %v: %v", query.Opcode, *query)
	}
	//Response Code必须为0
	if query.Rcode != 0 {
		// "In both multicast query and multicast response messages, the Response
		// Code MUST be zero on transmission.  Multicast DNS messages received with
		// non-zero Response Codes MUST be silently ignored."
		return fmt.Errorf("mdns: received query with non-zero Rcode %v: %v", query.Rcode, *query)
	}

	// TODO(reddaly): Handle "TC (Truncated) Bit":
	//    In query messages, if the TC bit is set, it means that additional
	//    Known-Answer records may be following shortly.  A responder SHOULD
	//    record this fact, and wait for those additional Known-Answer records,
	//    before deciding whether to respond.  If the TC bit is clear, it means
	//    that the querying host has no additional Known Answers.
	//支持消息截断
	if query.Truncated {
		return fmt.Errorf("[ERR] mdns: support for DNS requests with high truncated bit not implemented: %v", *query)
	}

	var unicastAnswer, multicastAnswer []dns.RR

	// Handle each question
	//依次处理query，区分请求是单播还是广播
	//单播：可能是请求对方服务，对方回复服务信息
	//广播：可能是局域网内有设备广播注册服务或者广播发现局域网内对应服务实例
	for _, q := range query.Question {
		//区分是单播还是广播
		mrecs, urecs := s.handleQuestion(q)
		//分别添加至单播列表和广播列表
		multicastAnswer = append(multicastAnswer, mrecs...)
		unicastAnswer = append(unicastAnswer, urecs...)
	}

	// See section 18 of RFC 6762 for rules about DNS headers.
	resp := func(unicast bool) *dns.Msg {
		// 18.1: ID (Query Identifier)
		// 0 for multicast response, query.Id for unicast response
		id := uint16(0)
		if unicast {
			id = query.Id
		}

		var answer []dns.RR
		//根据收到的类型为单播还是组内广播，设置对应的回复
		if unicast {
			answer = unicastAnswer
		} else {
			answer = multicastAnswer
		}
		if len(answer) == 0 {
			return nil
		}
		//生成resp信息
		//复制内容，回复给发送方
		return &dns.Msg{
			MsgHdr: dns.MsgHdr{
				Id: id,

				// 18.2: QR (Query/Response) Bit - must be set to 1 in response.
				Response: true,

				// 18.3: OPCODE - must be zero in response (OpcodeQuery == 0)
				Opcode: dns.OpcodeQuery,

				// 18.4: AA (Authoritative Answer) Bit - must be set to 1
				Authoritative: true,

				// The following fields must all be set to 0:
				// 18.5: TC (TRUNCATED) Bit
				// 18.6: RD (Recursion Desired) Bit
				// 18.7: RA (Recursion Available) Bit
				// 18.8: Z (Zero) Bit
				// 18.9: AD (Authentic Data) Bit
				// 18.10: CD (Checking Disabled) Bit
				// 18.11: RCODE (Response Code)
			},
			// 18.12 pertains to questions (handled by handleQuestion)
			// 18.13 pertains to resource records (handled by handleQuestion)

			// 18.14: Name Compression - responses should be compressed (though see
			// caveats in the RFC), so set the Compress bit (part of the dns library
			// API, not part of the DNS packet) to true.
			Compress: true,

			Answer: answer,
		}
	}
	//回复单播消息
	if mresp := resp(false); mresp != nil {
		if err := s.sendResponse(mresp, from); err != nil {
			return fmt.Errorf("mdns: error sending multicast response: %v", err)
		}
	}
	//回复广播消息
	if uresp := resp(true); uresp != nil {
		if err := s.sendResponse(uresp, from); err != nil {
			return fmt.Errorf("mdns: error sending unicast response: %v", err)
		}
	}
	return nil
}

// handleQuestion is used to handle an incoming question
//
// The response to a question may be transmitted over multicast, unicast, or
// both.  The return values are DNS records for each transmission type.
func (s *Server) handleQuestion(q dns.Question) (multicastRecs, unicastRecs []dns.RR) {
	//从本地MDNSService记录中查找对应记录
	records := s.config.Zone.Records(q)

	if len(records) == 0 {
		return nil, nil
	}

	// Handle unicast and multicast responses.
	// TODO(reddaly): The decision about sending over unicast vs. multicast is not
	// yet fully compliant with RFC 6762.  For example, the unicast bit should be
	// ignored if the records in question are close to TTL expiration.  For now,
	// we just use the unicast bit to make the decision, as per the spec:
	//     RFC 6762, section 18.12.  Repurposing of Top Bit of qclass in Question
	//     Section
	//
	//     In the Question Section of a Multicast DNS query, the top bit of the
	//     qclass field is used to indicate that unicast responses are preferred
	//     for this particular question.  (See Section 5.4.)
	//区分请求类型为unicast或者multicast
	if q.Qclass&(1<<15) != 0 {
		return nil, records
	}
	return records, nil
}

func (s *Server) probe() {
	defer s.wg.Done()

	sd, ok := s.config.Zone.(*MDNSService)
	if !ok {
		return
	}
	//获取 <service>.<transport>.<domain> 作为实例
	name := fmt.Sprintf("%s.%s.%s.", sd.Instance, trimDot(sd.Service), trimDot(sd.Domain))
	//组装mDNS包信息，包括如下3部分
	//PTR：主要包含服务实例名
	//SRV：包含服务的实例主机名以及端口
	//TXT：服务附加信息，关于服务更加详细的信息
	q := new(dns.Msg)
	q.SetQuestion(name, dns.TypePTR)
	q.RecursionDesired = false

	srv := &dns.SRV{
		Hdr: dns.RR_Header{
			Name:   name,
			Rrtype: dns.TypeSRV,
			Class:  dns.ClassINET,
			Ttl:    defaultTTL,
		},
		Priority: 0,
		Weight:   0,
		Port:     uint16(sd.Port),
		Target:   sd.HostName,
	}
	txt := &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   name,
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET,
			Ttl:    defaultTTL,
		},
		Txt: sd.TXT,
	}
	q.Ns = []dns.RR{srv, txt}

	randomizer := rand.New(rand.NewSource(time.Now().UnixNano()))
	//连续3次，间隔发送服务的mDNS包信息，以注册该服务
	for i := 0; i < 3; i++ {
		if err := s.SendMulticast(q); err != nil {
			log.Println("[ERR] mdns: failed to send probe:", err.Error())
		}
		time.Sleep(time.Duration(randomizer.Intn(250)) * time.Millisecond)
	}

	resp := new(dns.Msg)
	resp.MsgHdr.Response = true

	// set for query
	//类型为any的mDNS包主要用于查询局域网内是否有同名，如果没有，就以改名字作为自身域名
	q.SetQuestion(name, dns.TypeANY)

	resp.Answer = append(resp.Answer, s.config.Zone.Records(q.Question[0])...)

	// reset
	//PTR包用于查询局域网内对应服务实例名
	q.SetQuestion(name, dns.TypePTR)

	// From RFC6762
	//    The Multicast DNS responder MUST send at least two unsolicited
	//    responses, one second apart. To provide increased robustness against
	//    packet loss, a responder MAY send up to eight unsolicited responses,
	//    provided that the interval between unsolicited responses increases by
	//    at least a factor of two with every response sent.
	timeout := 1 * time.Second
	timer := time.NewTimer(timeout)
	//多次查询，每次间隔的时间翻倍
	for i := 0; i < 3; i++ {
		if err := s.SendMulticast(resp); err != nil {
			log.Println("[ERR] mdns: failed to send announcement:", err.Error())
		}
		select {
		case <-timer.C:
			timeout *= 2
			timer.Reset(timeout)
		case <-s.shutdownCh:
			timer.Stop()
			return
		}
	}
}

// multicastResponse us used to send a multicast response packet
func (s *Server) SendMulticast(msg *dns.Msg) error {
	buf, err := msg.Pack()
	if err != nil {
		return err
	}
	if s.ipv4List != nil {
		s.ipv4List.WriteToUDP(buf, ipv4Addr)
	}
	if s.ipv6List != nil {
		s.ipv6List.WriteToUDP(buf, ipv6Addr)
	}
	return nil
}

// sendResponse is used to send a response packet
//回复消息
func (s *Server) sendResponse(resp *dns.Msg, from net.Addr) error {
	// TODO(reddaly): Respect the unicast argument, and allow sending responses
	// over multicast.
	//装包数据
	buf, err := resp.Pack()
	if err != nil {
		return err
	}

	// Determine the socket to send from
	//解析对方地址
	addr := from.(*net.UDPAddr)
	//发送消息给对方
	if addr.IP.To4() != nil {
		_, err = s.ipv4List.WriteToUDP(buf, addr)
		return err
	} else {
		_, err = s.ipv6List.WriteToUDP(buf, addr)
		return err
	}
}

func (s *Server) unregister() error {
	sd, ok := s.config.Zone.(*MDNSService)
	if !ok {
		return nil
	}

	sd.TTL = 0
	name := fmt.Sprintf("%s.%s.%s.", sd.Instance, trimDot(sd.Service), trimDot(sd.Domain))

	q := new(dns.Msg)
	q.SetQuestion(name, dns.TypeANY)

	resp := new(dns.Msg)
	resp.MsgHdr.Response = true
	resp.Answer = append(resp.Answer, s.config.Zone.Records(q.Question[0])...)

	return s.SendMulticast(resp)
}
