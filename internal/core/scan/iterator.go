// Package scan: port + host iterator.
// Package scan: 端口 + 主机迭代器。
//
// Iterator streams (host, port) work items through a channel. It is
// independent of the Probe technique: any combination of hosts and
// ports can be scanned by iterating and feeding the channel into a
// Pool.
//
// Iterator 通过 channel 流式产出 (host, port) 工作项。它独立于探测技术：
// 任意主机和端口组合都能通过迭代产出并送入 Pool。
package scan

// Item is a single (host, port) work unit. / Item 是单个 (host, port) 工作单位。
type Item struct {
	Host string
	Port int
}

// Iterator produces Items. Implementations should be safe to consume
// concurrently from many goroutines (the channel-based default is).
//
// Iterator 产出 Item。实现应支持多 goroutine 并发消费（默认基于 channel）。
type Iterator interface {
	// Next returns the next Item, or (zero, false) when exhausted.
	// Next 返回下一个 Item；耗尽时返回 (zero, false)。
	Next() (Item, bool)
	// Estimated returns the rough total number of Items the iterator
	// will produce, or -1 if unknown. Used by the Pool to size its
	// internal channels. / Estimated 返回迭代器将产出的 Item 大致总数；
	// 未知返回 -1。Pool 用它来调内 channel 容量。
	Estimated() int
}

// ChanIterator wraps a Go channel as an Iterator. The channel is
// consumed lazily; close it to end iteration. / ChanIterator 把 Go
// channel 包成 Iterator。channel 懒消费；关闭表示迭代结束。
type ChanIterator struct {
	ch <-chan Item
}

// NewChanIterator returns a ChanIterator over the given channel.
// NewChanIterator 返回给定 channel 的 ChanIterator。
func NewChanIterator(ch <-chan Item) *ChanIterator { return &ChanIterator{ch: ch} }

// Next implements Iterator. / Next 实现 Iterator。
func (c *ChanIterator) Next() (Item, bool) {
	item, ok := <-c.ch
	return item, ok
}

// Estimated implements Iterator. Channels don't know their length;
// we conservatively return -1. / Estimated 实现 Iterator。channel
// 不知道自己的长度；保守返回 -1。
func (c *ChanIterator) Estimated() int { return -1 }

// CrossIterator produces the Cartesian product of a host list and a
// port list. / CrossIterator 产出主机列表和端口列表的笛卡尔积。
type CrossIterator struct {
	hosts  []string
	ports  []int
	totalN int
	i, j   int
}

// NewCrossIterator returns a CrossIterator that yields every
// (host, port) pair in (host-major, port-minor) order: hosts[0]×all
// ports, then hosts[1]×all ports, etc.
//
// NewCrossIterator 返回按"主机主序、端口次序"产出 (host, port) 对的
// CrossIterator：hosts[0]×所有端口，然后 hosts[1]×所有端口，依此类推。
func NewCrossIterator(hosts []string, ports []int) *CrossIterator {
	cp := make([]int, len(ports))
	copy(cp, ports)
	return &CrossIterator{
		hosts:  hosts,
		ports:  cp,
		totalN: len(hosts) * len(ports),
	}
}

// Next implements Iterator. / Next 实现 Iterator。
func (c *CrossIterator) Next() (Item, bool) {
	for c.i < len(c.hosts) {
		if c.j < len(c.ports) {
			item := Item{Host: c.hosts[c.i], Port: c.ports[c.j]}
			c.j++
			return item, true
		}
		c.i++
		c.j = 0
	}
	return Item{}, false
}

// Estimated implements Iterator. / Estimated 实现 Iterator。
func (c *CrossIterator) Estimated() int { return c.totalN }
