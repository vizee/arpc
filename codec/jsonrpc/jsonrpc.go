package jsonrpc

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/vizee/arpc"
)

type jsonRequest struct {
	Method string          `json:"method"`
	Id     uint64          `json:"id"`
	Params json.RawMessage `json:"params"`
}

type jsonError struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

type jsonResponse struct {
	Id     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *jsonError      `json:"error,omitempty"`
}

type Codec struct{}

func (Codec) Encode(obj interface{}) ([]byte, error) {
	return json.Marshal(obj)
}

func (Codec) Decode(src []byte, obj interface{}) error {
	return json.Unmarshal(src, obj)
}

type ServerConn[RWC io.ReadWriteCloser] struct {
	inner RWC
	enc   *json.Encoder
	dec   *json.Decoder
}

func splitNames(method string) (string, string) {
	pos := strings.LastIndexByte(method, '.')
	if pos >= 0 {
		return method[:pos], method[pos+1:]
	} else {
		return "", method
	}
}

func joinNames(service string, method string) string {
	return service + "." + method
}

func (sc *ServerConn[RWC]) ReadRequest(req *arpc.Request) error {
	var jreq jsonRequest
	err := sc.dec.Decode(&jreq)
	if err != nil {
		return err
	}
	if len(jreq.Method) != 0 && jreq.Method[0] == '#' {
		switch jreq.Method {
		case "#rpc.cancel":
			req.Opcode = arpc.OpCancel
			req.Seq = jreq.Id
		case "#rpc.shutdown":
			req.Opcode = arpc.OpShutdown
		default:
			req.Opcode = arpc.OpNop
		}
	} else {
		req.Opcode = arpc.OpCall
		req.Seq = jreq.Id
		req.Service, req.Method = splitNames(jreq.Method)
		req.Body = jreq.Params
	}
	return nil
}

func (sc *ServerConn[RWC]) WriteResponse(resp *arpc.Response) error {
	jresp := jsonResponse{
		Id: resp.Seq,
	}
	if resp.Code != arpc.CodeOK {
		jresp.Error = &jsonError{
			Code:    int(resp.Code),
			Message: string(resp.Body),
		}
	} else {
		jresp.Result = resp.Body
	}
	return sc.enc.Encode(&jresp)
}

func (sc *ServerConn[RWC]) Close() error {
	return sc.inner.Close()
}

func WrapServerConn[RWC io.ReadWriteCloser](rwc RWC) *ServerConn[RWC] {
	return &ServerConn[RWC]{
		inner: rwc,
		enc:   json.NewEncoder(rwc),
		dec:   json.NewDecoder(rwc),
	}
}

type ClientConn[RWC io.ReadWriteCloser] struct {
	rwc RWC
	enc *json.Encoder
	dec *json.Decoder
	wmu sync.Mutex
}

func (cc *ClientConn[RWC]) WriteRequest(req *arpc.Request) error {
	cc.wmu.Lock()
	defer cc.wmu.Unlock()

	switch req.Opcode {
	case arpc.OpCall:
		return cc.enc.Encode(&jsonRequest{
			Method: joinNames(req.Service, req.Method),
			Id:     req.Seq,
			Params: req.Body,
		})
	case arpc.OpCancel:
		return cc.enc.Encode(&jsonRequest{
			Method: "#rpc.cancel",
			Id:     req.Seq,
		})
	case arpc.OpShutdown:
		return cc.enc.Encode(&jsonRequest{
			Method: "#rpc.shutdown",
		})
	default:
		return fmt.Errorf("unsupport opcode: %d", req.Opcode)
	}
}

func (cc *ClientConn[RWC]) ReadResponse(resp *arpc.Response) error {
	var jresp jsonResponse
	err := cc.dec.Decode(&jresp)
	if err != nil {
		return err
	}
	if jresp.Error != nil {
		resp.Code = uint32(jresp.Error.Code)
		resp.Body = []byte(jresp.Error.Message)
	} else {
		resp.Code = arpc.CodeOK
		resp.Seq = jresp.Id
		resp.Body = jresp.Result
	}
	return nil
}

func (cc *ClientConn[RWC]) Close() error {
	return cc.rwc.Close()
}

func WrapClientConn[RWC io.ReadWriteCloser](rwc RWC) *ClientConn[RWC] {
	return &ClientConn[RWC]{
		rwc: rwc,
		enc: json.NewEncoder(rwc),
		dec: json.NewDecoder(rwc),
	}
}
