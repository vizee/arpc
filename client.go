package arpc

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

type ClientConn interface {
	// WriteRequest 负责向连接写入 Request，会被并发调用，需要保证并发安全
	WriteRequest(req *Request) error
	// ReadResponse 负责从连接读取 Response，不会并发调用
	ReadResponse(resp *Response) error
	Close() error
}

type Client[CC ClientConn, E Codec] struct {
	state   int32 // 0-initial; 1-running; 2-down
	wg      sync.WaitGroup
	codec   E
	conn    CC
	seq     uint64
	pending map[uint64]chan *Response
	pmu     sync.Mutex
	err     error
}

func (c *Client[CC, E]) shutdown(err error, graceful bool) bool {
	c.pmu.Lock()
	if atomic.LoadInt32(&c.state) == 2 {
		c.pmu.Unlock()
		return false
	}
	c.err = err
	// 更新 state 后其他协程可以安全地访问 err
	atomic.StoreInt32(&c.state, 2)

	// 确保其他协程不会再访问 pending
	var pending map[uint64]chan *Response
	if !graceful {
		pending = c.pending
		c.pending = nil
	}
	c.pmu.Unlock()

	for _, ch := range pending {
		close(ch)
	}
	if !graceful {
		c.conn.Close()
	}
	return true
}

func (c *Client[CC, E]) bg() {
	for {
		var resp Response
		err := c.conn.ReadResponse(&resp)
		if err != nil {
			c.shutdown(err, false)
			break
		}

		c.pmu.Lock()
		ch := c.pending[resp.Seq]
		if ch != nil {
			delete(c.pending, resp.Seq)
		}
		npending := len(c.pending)
		c.pmu.Unlock()
		// 可能存在请求已经被取消，但仍然收到 response 的情况
		if ch != nil {
			ch <- &resp
		}
		// 如果没有 pending 中的请求且 state 为 down，bg 可以安全退出
		if npending == 0 && atomic.LoadInt32(&c.state) == 2 {
			break
		}
	}
	c.conn.Close()
	c.wg.Done()
}

func (c *Client[CC, E]) cancelRequest(seq uint64) error {
	c.pmu.Lock()
	delete(c.pending, seq)
	c.pmu.Unlock()
	return c.conn.WriteRequest(&Request{
		Opcode: OpCancel,
		Seq:    seq,
	})
}

func (c *Client[CC, E]) GracefulClose() error {
	err := c.conn.WriteRequest(&Request{Opcode: OpShutdown})
	if err != nil {
		return err
	}
	if !c.shutdown(&Error{Code: CodeConnClosed}, true) {
		return c.err
	}
	c.wg.Wait()
	return nil
}
func (c *Client[CC, E]) Close() error {
	if !c.shutdown(&Error{Code: CodeConnClosed}, false) {
		return c.err
	}
	return nil
}

func NewClient[CC ClientConn, E Codec](conn CC, codec E) *Client[CC, E] {
	return &Client[CC, E]{
		conn:    conn,
		codec:   codec,
		pending: make(map[uint64]chan *Response),
	}
}

func Invoke[CC ClientConn, E Codec, I, O any](ac *Client[CC, E], ctx context.Context, service string, method string, args *I, reply *O) error {
	body, err := ac.codec.Encode(args)
	if err != nil {
		return err
	}

	ch := make(chan *Response, 1)
	var seq uint64
	ac.pmu.Lock()
	clientState := atomic.LoadInt32(&ac.state)
	if clientState != 2 {
		// 在锁内访问 state，只要 state 不处于 down，pending 内容都会被正确处理
		ac.seq++
		seq = ac.seq
		ac.pending[seq] = ch
	}
	ac.pmu.Unlock()

	switch clientState {
	case 0:
		if atomic.CompareAndSwapInt32(&ac.state, 0, 1) {
			ac.wg.Add(1)
			go ac.bg()
		}
	case 2:
		return ac.err
	}

	// 如果请求成功，等待 bg 协程触发，否则手动移除 pending

	err = ac.conn.WriteRequest(&Request{
		Opcode:  OpCall,
		Seq:     seq,
		Service: service,
		Method:  method,
		Body:    body,
	})
	if err != nil {
		ac.pmu.Lock()
		delete(ac.pending, seq)
		ac.pmu.Unlock()
		return err
	}

	select {
	case resp := <-ch:
		if resp == nil {
			return ac.err
		}
		if resp.Code != CodeOK {
			return &Error{
				Code:    resp.Code,
				Message: string(resp.Body),
			}
		}
		return ac.codec.Decode(resp.Body, reply)
	case <-ctx.Done():
		// 取消请求需要通知服务端，但是没办法处理发生取消通知的失败
		err := ac.cancelRequest(seq)
		if err != nil {
			return fmt.Errorf("cancel request: %w", err)
		}
		return ctx.Err()
	}
}
