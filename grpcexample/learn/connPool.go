package learn

//连接池,摘自rpc_pool.go
/*
连接池是一个addr-sockfd的map,
在短时间内维持连接,避免频繁的连接;
超过生命周期后,释放连接,所以使用TTL来计算生命周期
如果生存时间大于TTL,则释放该连接
*/
/*
func (p *pool) getConn(addr string, tr transport.Transport, opts ...transport.DialOption) (*poolConn, error) {
	p.Lock()
	conns := p.conns[addr]
	now := time.Now().Unix()

	// while we have conns check age and then return one
	// otherwise we'll create a new conn
	for len(conns) > 0 {
		conn := conns[len(conns)-1]
		conns = conns[:len(conns)-1]
		p.conns[addr] = conns

		// if conn is old kill it and move on
		if d := now - conn.created; d > p.ttl {
			conn.Client.Close()
			continue
		}

		// we got a good conn, lets unlock and return it
		p.Unlock()

		return conn, nil
	}

	p.Unlock()

	// create new conn
	c, err := tr.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}
	return &poolConn{c, time.Now().Unix()}, nil
}
*/
