package arpc

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
)

const (
	OpNop uint8 = iota
	OpCall
	OpCancel
	OpShutdown
)

type Codec interface {
	Encode(obj interface{}) ([]byte, error)
	Decode(src []byte, obj interface{}) error
}

type Request struct {
	Opcode  uint8
	Seq     uint64
	Service string
	Method  string
	Body    []byte
	ctx     requestContext
}

func (r *Request) getContext() context.Context {
	if r.ctx.inner == nil {
		return context.Background()
	} else {
		return r.ctx.inner
	}
}

func NewRequest(ctx context.Context) *Request {
	return &Request{
		ctx: requestContext{
			inner: ctx,
		},
	}
}

type Response struct {
	Seq  uint64
	Code uint32
	Body []byte
}

func makeResponse(seq uint64, code uint32, body []byte) *Response {
	return &Response{
		Seq:  seq,
		Code: code,
		Body: body,
	}
}

type ServerConn interface {
	ReadRequest(req *Request) error
	WriteResponse(resp *Response) error
	Close() error
}

type Server[SC ServerConn, E Codec] struct {
	codec    E
	services map[string]*serviceDesc
}

func (s *Server[SC, E]) Handle(req *Request) (interface{}, *Error) {
	svc := s.services[req.Service]
	if svc == nil {
		return nil, &Error{Code: CodeServiceNotFound}
	}
	meth := svc.methods[req.Method]
	if meth == nil {
		return nil, &Error{Code: CodeMethodNotFound}
	}
	in := reflect.New(meth.in)
	err := s.codec.Decode(req.Body, in.Interface())
	if err != nil {
		return nil, &Error{Code: CodeInvalidRequest, Message: err.Error()}
	}
	ctx := req.getContext()
	reply := meth.fn.Call([]reflect.Value{svc.rcvr, reflect.ValueOf(ctx), in})
	if !reply[1].IsNil() {
		return nil, AsError(reply[1].Interface().(error))
	}
	return reply[0].Interface(), nil
}

func (s *Server[SC, E]) asyncHandleRequest(sc *serverConn[SC]) {
	for req := range sc.rq {
		// 如果请求已经取消，直接跳过请求，这样可以少一次 pmu 锁
		if req.ctx.interrupted() {
			continue
		}
		sc.pmu.Lock()
		delete(sc.pending, req.Seq)
		sc.pmu.Unlock()

		// 即使这时候请求取消，继续向下分发
		res, e := s.Handle(req)
		var resp *Response
		if e != nil {
			resp = makeResponse(req.Seq, e.Code, []byte(e.Message))
		} else {
			body, err := s.codec.Encode(res)
			if err != nil {
				sc.shutdown(err)
				break
			}
			resp = makeResponse(req.Seq, CodeOK, body)
		}
		err := sc.conn.WriteResponse(resp)
		if err != nil {
			sc.shutdown(err)
			break
		}
	}
	sc.conn.Close()
	sc.wg.Done()
}

func (s *Server[SC, E]) ServeConn(conn SC) error {
	sc := &serverConn[SC]{
		conn:    conn,
		rq:      make(chan *Request, 64),
		pending: make(map[uint64]*Request),
	}
	rootCtx, cancelAll := context.WithCancel(context.Background())
	// TODO: allows multiple goroutines for asyncHandleRequest
	sc.wg.Add(1)
	go s.asyncHandleRequest(sc)

	graceful := false
	for atomic.LoadInt32(&sc.down) == 0 {
		req := new(Request)
		err := sc.conn.ReadRequest(req)
		if err != nil {
			sc.shutdown(err)
			break
		}
		switch req.Opcode {
		case OpNop:
		case OpCall:
			req.ctx.inner, req.ctx.cancel = context.WithCancel(rootCtx)
			sc.pmu.Lock()
			sc.pending[req.Seq] = req
			sc.pmu.Unlock()
			sc.rq <- req
		case OpCancel:
			sc.pmu.Lock()
			pr := sc.pending[req.Seq]
			if pr != nil {
				delete(sc.pending, pr.Seq)
			}
			sc.pmu.Unlock()
			if pr != nil {
				pr.ctx.cancel()
			}
		case OpShutdown:
			graceful = true
			sc.shutdown(nil)
		default:
			sc.shutdown(fmt.Errorf("unrecognized opcode: %d", req.Opcode))
		}
	}

	if graceful {
		// 如果优雅退出，先等待协程退出后再调用 cancelAll（本质 no-op）
		sc.wg.Wait()
		cancelAll()
	} else {
		// 如果故障退出，先取消所有正在处理的请求，再等待处理请求的协程退出
		cancelAll()
		sc.wg.Wait()
	}

	// wg.Wait() 可以保证 sc.err 不竞争
	return sc.err
}

type serverConn[SC ServerConn] struct {
	down    int32
	err     error
	conn    ServerConn
	rq      chan *Request
	wg      sync.WaitGroup
	pending map[uint64]*Request
	pmu     sync.Mutex
}

func (sc *serverConn[SC]) shutdown(err error) {
	if atomic.CompareAndSwapInt32(&sc.down, 0, 1) {
		sc.err = err
		close(sc.rq)
	}
}

type requestContext struct {
	inner  context.Context
	cancel context.CancelFunc
}

func (r *requestContext) interrupted() bool {
	return r.inner.Err() != nil
}
