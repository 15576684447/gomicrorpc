package mdnsdemo

import (
	"bytes"
	"net"
	"reflect"
	"testing"

	"github.com/micro/mdns"
	"github.com/miekg/dns"
)

func makeService(t *testing.T) *mdns.MDNSService {
	return makeServiceWithServiceName(t, "_http._tcp")
}

func makeServiceWithServiceName(t *testing.T, service string) *mdns.MDNSService {
	m, err := mdns.NewMDNSService(
		"hostname",
		service,
		"local.",
		"testhost.",
		80, // port
		[]net.IP{net.IP([]byte{192, 168, 0, 42}), net.ParseIP("2620:0:1000:1900:b0c2:d0b2:c411:18bc")},
		[]string{"Local web server"}) // TXT

	if err != nil {
		t.Fatalf("err: %v", err)
	}

	return m
}

func TestNewMDNSService_BadParams(t *testing.T) {
	for _, test := range []struct {
		testName string
		hostName string
		domain   string
	}{
		{
			"NewMDNSService should fail when passed hostName that is not a legal fully-qualified domain name",
			"hostname", // not legal FQDN - should be "hostname." or "hostname.local.", etc.
			"local.",   // legal
		},
		{
			"NewMDNSService should fail when passed domain that is not a legal fully-qualified domain name",
			"hostname.", // legal
			"local",     // should be "local."
		},
	} {
		_, err := mdns.NewMDNSService(
			"instance name",
			"_http._tcp",
			test.domain,
			test.hostName,
			80, // port
			[]net.IP{net.IP([]byte{192, 168, 0, 42})},
			[]string{"Local web server"}) // TXT
		if err == nil {
			t.Fatalf("%s: error expected, but got none", test.testName)
		}
	}
}
//给了不正确的addr，返回必然为空，如果返回不为空，则测试失败
func TestMDNSService_BadAddr(t *testing.T) {
	s := makeService(t)
	q := dns.Question{
		Name:  "random",
		//Name:  "_http._tcp.local.",
		Qtype: dns.TypeANY,
	}
	recs := s.Records(q)
	t.Logf("%+v\n", recs)
	if len(recs) != 0 {
		t.Fatalf("bad: %v", recs)
	}
}

/*
程序执行返回

	zone_test.go:97: PTR: _http._tcp.local.	120	IN	PTR	hostname._http._tcp.local.
    zone_test.go:98: PTR.Ptr: hostname._http._tcp.local.
    zone_test.go:104: SVR: hostname._http._tcp.local.	120	IN	SRV	10 1 80 testhost.
    zone_test.go:109: A: testhost.	120	IN	A	192.168.0.42
    zone_test.go:114: AAA: testhost.	120	IN	AAAA	2620:0:1000:1900:b0c2:d0b2:c411:18bc
    zone_test.go:119: TXT: hostname._http._tcp.local.	120	IN	TXT	"Local web server"
 */
/*
PTR: 包含Ptr，由 instance.service.hostName 组成
SVR: 包含Port和Target信息，Target即服务名
A: 包含了IPV4信息
AAAA: 包含了IPV6信息
TXT: 包含了附加信息
 */
//使用serviceAddr
func TestMDNSService_ServiceAddr(t *testing.T) {
	s := makeService(t)
	q := dns.Question{
		Name:  "_http._tcp.local.",
		Qtype: dns.TypeANY,
	}
	recs := s.Records(q)
	//PTR,SVR,A,AAAA,TXT
	if got, want := len(recs), 5; got != want {
		t.Fatalf("got %d records, want %d: %v", got, want, recs)
	}
	t.Logf("Records: %+v\n", recs)

	if ptr, ok := recs[0].(*dns.PTR); !ok {
		t.Errorf("recs[0] should be PTR record, got: %v, all records: %v", recs[0], recs)
	} else if got, want := ptr.Ptr, "hostname._http._tcp.local."; got != want {
		t.Fatalf("bad PTR record %v: got %v, want %v", ptr, got, want)
	}
	ptr, _ := recs[0].(*dns.PTR)
	t.Logf("PTR: %+v\n", ptr)
	t.Logf("PTR.Ptr: %s\n", ptr.Ptr)

	if _, ok := recs[1].(*dns.SRV); !ok {
		t.Errorf("recs[1] should be SRV record, got: %v, all reccords: %v", recs[1], recs)
	}
	svr, _ := recs[1].(*dns.SRV)
	t.Logf("SVR: %+v\n", svr)
	if _, ok := recs[2].(*dns.A); !ok {
		t.Errorf("recs[2] should be A record, got: %v, all records: %v", recs[2], recs)
	}
	a, _ := recs[2].(*dns.A)
	t.Logf("A: %+v\n", a)
	if _, ok := recs[3].(*dns.AAAA); !ok {
		t.Errorf("recs[3] should be AAAA record, got: %v, all records: %v", recs[3], recs)
	}
	aaa, _ := recs[3].(*dns.AAAA)
	t.Logf("AAA: %+v\n", aaa)
	if _, ok := recs[4].(*dns.TXT); !ok {
		t.Errorf("recs[4] should be TXT record, got: %v, all records: %v", recs[4], recs)
	}
	txt, _ := recs[4].(*dns.TXT)
	t.Logf("TXT: %+v\n", txt)

	q.Qtype = dns.TypePTR
	if recs2 := s.Records(q); !reflect.DeepEqual(recs, recs2) {
		t.Fatalf("PTR question should return same result as ANY question: ANY => %v, PTR => %v", recs, recs2)
	}
}

//使用ServiceAddr作为name时，Qtype只能为TypeANY ?????
func TestMDNSService_ServiceAddr_SRV(t *testing.T) {
	s := makeService(t)
	q := dns.Question{
		Name:  "_http._tcp.local.",
		//Qtype: dns.TypePTR,//len=5
		//Qtype: dns.TypeSRV,//len=0
		//Qtype: dns.TypeA,//len=0
		//Qtype: dns.TypeAAAA,//len=0
		//Qtype: dns.TypeTXT,//len=0
		Qtype: dns.TypeANY,//len=5
	}
	recs := s.Records(q)
	t.Logf("len=%d\n", len(recs))
}

//使用instanceAddr，type=ANY
func TestMDNSService_InstanceAddr_ANY(t *testing.T) {
	s := makeService(t)
	q := dns.Question{
		Name:  "hostname._http._tcp.local.",
		Qtype: dns.TypeANY,
	}
	recs := s.Records(q)
	t.Logf("%+v\n", recs)
	if len(recs) != 4 {
		t.Fatalf("bad: %v", recs)
	}
	if _, ok := recs[0].(*dns.SRV); !ok {
		t.Fatalf("bad: %v", recs[0])
	}
	if _, ok := recs[1].(*dns.A); !ok {
		t.Fatalf("bad: %v", recs[1])
	}
	if _, ok := recs[2].(*dns.AAAA); !ok {
		t.Fatalf("bad: %v", recs[2])
	}
	if _, ok := recs[3].(*dns.TXT); !ok {
		t.Fatalf("bad: %v", recs[3])
	}
}
//使用instanceAddr，type=SRV
func TestMDNSService_InstanceAddr_SRV(t *testing.T) {
	s := makeService(t)
	q := dns.Question{
		Name:  "hostname._http._tcp.local.",
		Qtype: dns.TypeSRV,
	}
	recs := s.Records(q)
	if len(recs) != 3 {
		t.Fatalf("bad: %v", recs)
	}
	srv, ok := recs[0].(*dns.SRV)
	if !ok {
		t.Fatalf("bad: %v", recs[0])
	}
	if _, ok := recs[1].(*dns.A); !ok {
		t.Fatalf("bad: %v", recs[1])
	}
	if _, ok := recs[2].(*dns.AAAA); !ok {
		t.Fatalf("bad: %v", recs[2])
	}

	if srv.Port != uint16(s.Port) {
		t.Fatalf("bad: %v", recs[0])
	}
}
//使用instanceAddr，type=A
func TestMDNSService_InstanceAddr_A(t *testing.T) {
	s := makeService(t)
	q := dns.Question{
		Name:  "hostname._http._tcp.local.",
		Qtype: dns.TypeA,
	}
	recs := s.Records(q)
	if len(recs) != 1 {
		t.Fatalf("bad: %v", recs)
	}
	a, ok := recs[0].(*dns.A)
	if !ok {
		t.Fatalf("bad: %v", recs[0])
	}
	if !bytes.Equal(a.A, []byte{192, 168, 0, 42}) {
		t.Fatalf("bad: %v", recs[0])
	}
}
//使用instanceAddr，type=AAAA
func TestMDNSService_InstanceAddr_AAAA(t *testing.T) {
	s := makeService(t)
	q := dns.Question{
		Name:  "hostname._http._tcp.local.",
		Qtype: dns.TypeAAAA,
	}
	recs := s.Records(q)
	if len(recs) != 1 {
		t.Fatalf("bad: %v", recs)
	}
	a4, ok := recs[0].(*dns.AAAA)
	if !ok {
		t.Fatalf("bad: %v", recs[0])
	}
	ip6 := net.ParseIP("2620:0:1000:1900:b0c2:d0b2:c411:18bc")
	if got := len(ip6); got != net.IPv6len {
		t.Fatalf("test IP failed to parse (len = %d, want %d)", got, net.IPv6len)
	}
	if !bytes.Equal(a4.AAAA, ip6) {
		t.Fatalf("bad: %v", recs[0])
	}
}
//使用instanceAddr，type=TXT
func TestMDNSService_InstanceAddr_TXT(t *testing.T) {
	s := makeService(t)
	q := dns.Question{
		Name:  "hostname._http._tcp.local.",
		Qtype: dns.TypeTXT,
	}
	recs := s.Records(q)
	if len(recs) != 1 {
		t.Fatalf("bad: %v", recs)
	}
	txt, ok := recs[0].(*dns.TXT)
	if !ok {
		t.Fatalf("bad: %v", recs[0])
	}
	if got, want := txt.Txt, s.TXT; !reflect.DeepEqual(got, want) {
		t.Fatalf("TXT record mismatch for %v: got %v, want %v", recs[0], got, want)
	}
}

func TestMDNSService_HostNameQuery(t *testing.T) {
	s := makeService(t)
	for _, test := range []struct {
		q    dns.Question
		want []dns.RR
	}{
		{
			dns.Question{Name: "testhost.", Qtype: dns.TypeA},
			[]dns.RR{&dns.A{
				Hdr: dns.RR_Header{
					Name:   "testhost.",
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    120,
				},
				A: net.IP([]byte{192, 168, 0, 42}),
			}},
		},
		{
			dns.Question{Name: "testhost.", Qtype: dns.TypeAAAA},
			[]dns.RR{&dns.AAAA{
				Hdr: dns.RR_Header{
					Name:   "testhost.",
					Rrtype: dns.TypeAAAA,
					Class:  dns.ClassINET,
					Ttl:    120,
				},
				AAAA: net.ParseIP("2620:0:1000:1900:b0c2:d0b2:c411:18bc"),
			}},
		},
	} {
		if got := s.Records(test.q); !reflect.DeepEqual(got, test.want) {
			t.Errorf("hostname query failed: s.Records(%v) = %v, want %v", test.q, got, test.want)
		}
	}
}

func TestMDNSService_serviceEnum_PTR(t *testing.T) {
	s := makeService(t)
	q := dns.Question{
		Name:  "_services._dns-sd._udp.local.",
		Qtype: dns.TypePTR,
	}
	recs := s.Records(q)
	if len(recs) != 1 {
		t.Fatalf("bad: %v", recs)
	}
	if ptr, ok := recs[0].(*dns.PTR); !ok {
		t.Errorf("recs[0] should be PTR record, got: %v, all records: %v", recs[0], recs)
	} else if got, want := ptr.Ptr, "_http._tcp.local."; got != want {
		t.Fatalf("bad PTR record %v: got %v, want %v", ptr, got, want)
	}
}
