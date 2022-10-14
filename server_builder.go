package arpc

import (
	"context"
	"fmt"
	"reflect"
)

type methodDesc struct {
	fn reflect.Value
	in reflect.Type
}

type serviceDesc struct {
	rcvr    reflect.Value
	methods map[string]*methodDesc
}

type ServerBuilder[SC ServerConn, E Codec] struct {
	codec    E
	services map[string]*serviceDesc
	err      error
}

func (b *ServerBuilder[SC, E]) WithCodec(codec E) *ServerBuilder[SC, E] {
	if b.err != nil {
		return b
	}
	b.codec = codec
	return b
}

func (b *ServerBuilder[SC, E]) registerService(name string, sv reflect.Value, stype reflect.Type) *ServerBuilder[SC, E] {
	if b.services[name] != nil {
		b.err = fmt.Errorf("service %s already defined", name)
		return b
	}

	methods := collectRpcMethods(stype)
	if len(methods) == 0 {
		b.err = fmt.Errorf("type %s has no suitable RPC method", stype.Name())
		return b
	}

	if b.services == nil {
		b.services = make(map[string]*serviceDesc)
	}
	b.services[name] = &serviceDesc{
		rcvr:    sv,
		methods: methods,
	}
	return b
}

func (b *ServerBuilder[SC, E]) RegisterNamed(name string, svc interface{}) *ServerBuilder[SC, E] {
	if b.err != nil {
		return b
	}

	sv := reflect.ValueOf(svc)
	return b.registerService(name, sv, sv.Type())
}

func (b *ServerBuilder[SC, E]) Register(svc interface{}) *ServerBuilder[SC, E] {
	if b.err != nil {
		return b
	}

	sv := reflect.ValueOf(svc)
	stype := sv.Type()

	var name string
	if naming, ok := svc.(interface{ ServiceName() string }); ok {
		name = naming.ServiceName()
	} else {
		ind := stype
		if ind.Kind() == reflect.Ptr {
			ind = stype.Elem()
		}
		name = ind.Name()
	}

	return b.registerService(name, sv, stype)
}

func (b *ServerBuilder[SC, E]) Build() (*Server[SC, E], error) {
	if b.err != nil {
		return nil, b.err
	}
	return &Server[SC, E]{
		codec:    b.codec,
		services: b.services,
	}, nil
}

var (
	contextType = reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType   = reflect.TypeOf((*error)(nil)).Elem()
)

func collectRpcMethods(rcvrType reflect.Type) map[string]*methodDesc {
	methods := make(map[string]*methodDesc)
	for i := 0; i < rcvrType.NumMethod(); i++ {
		// method 原型需要满足:
		// func (*Service) MethodName(context.Context, *ArgsStructType) (*ReplyStructType, error)
		m := rcvrType.Method(i)
		if !m.IsExported() {
			continue
		}
		mty := m.Type
		if mty.NumIn() != 3 || mty.NumOut() != 2 {
			continue
		}
		if mty.In(1) != contextType || mty.Out(1) != errorType {
			continue
		}
		in := mty.In(2)
		out := mty.Out(0)
		if in.Kind() != reflect.Ptr || in.Elem().Kind() != reflect.Struct ||
			out.Kind() != reflect.Ptr || out.Elem().Kind() != reflect.Struct {
			continue
		}
		methods[m.Name] = &methodDesc{
			fn: m.Func,
			in: in.Elem(),
		}
	}
	return methods
}

func NewServerBuilder[SC ServerConn, E Codec]() *ServerBuilder[SC, E] {
	return &ServerBuilder[SC, E]{}
}
