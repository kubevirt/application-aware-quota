// Code generated by protoc-gen-go. DO NOT EDIT.
// source: staging/src/kubevirt.io/application-aware-quota-api/libsidecar/evaluator-server-com/evaluate.proto

/*
Package aaq_sidecar_evaluate is a generated protocol buffer package.

It is generated from these files:

	staging/src/kubevirt.io/application-aware-quota-api/libsidecar/evaluator-server-com/evaluate.proto

It has these top-level messages:

	PodUsageRequest
	PodUsageResponse
	Error
	HealthCheckRequest
	HealthCheckResponse
	ResourceList
	Pod
*/
package aaq_sidecar_evaluate

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

import (
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

// Define messages
type PodUsageRequest struct {
	Pod       *Pod   `protobuf:"bytes,1,opt,name=pod" json:"pod,omitempty"`
	PodsState []*Pod `protobuf:"bytes,2,rep,name=podsState" json:"podsState,omitempty"`
}

func (m *PodUsageRequest) Reset()                    { *m = PodUsageRequest{} }
func (m *PodUsageRequest) String() string            { return proto.CompactTextString(m) }
func (*PodUsageRequest) ProtoMessage()               {}
func (*PodUsageRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

func (m *PodUsageRequest) GetPod() *Pod {
	if m != nil {
		return m.Pod
	}
	return nil
}

func (m *PodUsageRequest) GetPodsState() []*Pod {
	if m != nil {
		return m.PodsState
	}
	return nil
}

type PodUsageResponse struct {
	ResourceList *ResourceList `protobuf:"bytes,1,opt,name=resourceList" json:"resourceList,omitempty"`
	Match        bool          `protobuf:"varint,2,opt,name=match" json:"match,omitempty"`
	Error        *Error        `protobuf:"bytes,3,opt,name=error" json:"error,omitempty"`
}

func (m *PodUsageResponse) Reset()                    { *m = PodUsageResponse{} }
func (m *PodUsageResponse) String() string            { return proto.CompactTextString(m) }
func (*PodUsageResponse) ProtoMessage()               {}
func (*PodUsageResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

func (m *PodUsageResponse) GetResourceList() *ResourceList {
	if m != nil {
		return m.ResourceList
	}
	return nil
}

func (m *PodUsageResponse) GetMatch() bool {
	if m != nil {
		return m.Match
	}
	return false
}

func (m *PodUsageResponse) GetError() *Error {
	if m != nil {
		return m.Error
	}
	return nil
}

type Error struct {
	Error        bool   `protobuf:"varint,1,opt,name=error" json:"error,omitempty"`
	ErrorMessage string `protobuf:"bytes,2,opt,name=errorMessage" json:"errorMessage,omitempty"`
}

func (m *Error) Reset()                    { *m = Error{} }
func (m *Error) String() string            { return proto.CompactTextString(m) }
func (*Error) ProtoMessage()               {}
func (*Error) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{2} }

func (m *Error) GetError() bool {
	if m != nil {
		return m.Error
	}
	return false
}

func (m *Error) GetErrorMessage() string {
	if m != nil {
		return m.ErrorMessage
	}
	return ""
}

type HealthCheckRequest struct {
}

func (m *HealthCheckRequest) Reset()                    { *m = HealthCheckRequest{} }
func (m *HealthCheckRequest) String() string            { return proto.CompactTextString(m) }
func (*HealthCheckRequest) ProtoMessage()               {}
func (*HealthCheckRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{3} }

type HealthCheckResponse struct {
	Healthy bool `protobuf:"varint,1,opt,name=healthy" json:"healthy,omitempty"`
}

func (m *HealthCheckResponse) Reset()                    { *m = HealthCheckResponse{} }
func (m *HealthCheckResponse) String() string            { return proto.CompactTextString(m) }
func (*HealthCheckResponse) ProtoMessage()               {}
func (*HealthCheckResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{4} }

func (m *HealthCheckResponse) GetHealthy() bool {
	if m != nil {
		return m.Healthy
	}
	return false
}

type ResourceList struct {
	ResourceListJson []byte `protobuf:"bytes,1,opt,name=resourceListJson,proto3" json:"resourceListJson,omitempty"`
}

func (m *ResourceList) Reset()                    { *m = ResourceList{} }
func (m *ResourceList) String() string            { return proto.CompactTextString(m) }
func (*ResourceList) ProtoMessage()               {}
func (*ResourceList) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{5} }

func (m *ResourceList) GetResourceListJson() []byte {
	if m != nil {
		return m.ResourceListJson
	}
	return nil
}

type Pod struct {
	PodJson []byte `protobuf:"bytes,1,opt,name=podJson,proto3" json:"podJson,omitempty"`
}

func (m *Pod) Reset()                    { *m = Pod{} }
func (m *Pod) String() string            { return proto.CompactTextString(m) }
func (*Pod) ProtoMessage()               {}
func (*Pod) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{6} }

func (m *Pod) GetPodJson() []byte {
	if m != nil {
		return m.PodJson
	}
	return nil
}

func init() {
	proto.RegisterType((*PodUsageRequest)(nil), "evaluate.PodUsageRequest")
	proto.RegisterType((*PodUsageResponse)(nil), "evaluate.PodUsageResponse")
	proto.RegisterType((*Error)(nil), "evaluate.Error")
	proto.RegisterType((*HealthCheckRequest)(nil), "evaluate.HealthCheckRequest")
	proto.RegisterType((*HealthCheckResponse)(nil), "evaluate.HealthCheckResponse")
	proto.RegisterType((*ResourceList)(nil), "evaluate.ResourceList")
	proto.RegisterType((*Pod)(nil), "evaluate.Pod")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// Client API for PodUsage service

type PodUsageClient interface {
	PodUsageFunc(ctx context.Context, in *PodUsageRequest, opts ...grpc.CallOption) (*PodUsageResponse, error)
	HealthCheck(ctx context.Context, in *HealthCheckRequest, opts ...grpc.CallOption) (*HealthCheckResponse, error)
}

type podUsageClient struct {
	cc *grpc.ClientConn
}

func NewPodUsageClient(cc *grpc.ClientConn) PodUsageClient {
	return &podUsageClient{cc}
}

func (c *podUsageClient) PodUsageFunc(ctx context.Context, in *PodUsageRequest, opts ...grpc.CallOption) (*PodUsageResponse, error) {
	out := new(PodUsageResponse)
	err := grpc.Invoke(ctx, "/evaluate.PodUsage/PodUsageFunc", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *podUsageClient) HealthCheck(ctx context.Context, in *HealthCheckRequest, opts ...grpc.CallOption) (*HealthCheckResponse, error) {
	out := new(HealthCheckResponse)
	err := grpc.Invoke(ctx, "/evaluate.PodUsage/HealthCheck", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Server API for PodUsage service

type PodUsageServer interface {
	PodUsageFunc(context.Context, *PodUsageRequest) (*PodUsageResponse, error)
	HealthCheck(context.Context, *HealthCheckRequest) (*HealthCheckResponse, error)
}

func RegisterPodUsageServer(s *grpc.Server, srv PodUsageServer) {
	s.RegisterService(&_PodUsage_serviceDesc, srv)
}

func _PodUsage_PodUsageFunc_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(PodUsageRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PodUsageServer).PodUsageFunc(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/evaluate.PodUsage/PodUsageFunc",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PodUsageServer).PodUsageFunc(ctx, req.(*PodUsageRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _PodUsage_HealthCheck_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(HealthCheckRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PodUsageServer).HealthCheck(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/evaluate.PodUsage/HealthCheck",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PodUsageServer).HealthCheck(ctx, req.(*HealthCheckRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _PodUsage_serviceDesc = grpc.ServiceDesc{
	ServiceName: "evaluate.PodUsage",
	HandlerType: (*PodUsageServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "PodUsageFunc",
			Handler:    _PodUsage_PodUsageFunc_Handler,
		},
		{
			MethodName: "HealthCheck",
			Handler:    _PodUsage_HealthCheck_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "staging/src/kubevirt.io/application-aware-quota-api/libsidecar/evaluator-server-com/evaluate.proto",
}

func init() {
	proto.RegisterFile("staging/src/kubevirt.io/application-aware-quota-api/libsidecar/evaluator-server-com/evaluate.proto", fileDescriptor0)
}

var fileDescriptor0 = []byte{
	// 407 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x74, 0x52, 0xdd, 0x6a, 0xd4, 0x40,
	0x14, 0x26, 0x5d, 0x56, 0xb7, 0xa7, 0x91, 0x96, 0x71, 0x29, 0x71, 0x51, 0xba, 0x04, 0x84, 0xa2,
	0x64, 0x03, 0xf5, 0xae, 0x77, 0x2a, 0x15, 0x29, 0x0a, 0x4b, 0xc4, 0x1b, 0x6f, 0xe4, 0xec, 0xe4,
	0xb0, 0x3b, 0x74, 0x9b, 0x33, 0x3b, 0x33, 0x59, 0xf1, 0x19, 0x7c, 0x06, 0xdf, 0x55, 0x32, 0x49,
	0x9a, 0x84, 0xb5, 0x77, 0xf9, 0x7e, 0xe6, 0x3b, 0xf9, 0xce, 0x0c, 0xac, 0xac, 0xc3, 0xb5, 0x2a,
	0xd6, 0xa9, 0x35, 0x32, 0xbd, 0x2b, 0x57, 0xb4, 0x57, 0xc6, 0x2d, 0x14, 0xa7, 0xa8, 0xf5, 0x56,
	0x49, 0x74, 0x8a, 0x8b, 0x04, 0x7f, 0xa1, 0xa1, 0x64, 0x57, 0xb2, 0xc3, 0x04, 0xb5, 0x4a, 0xb7,
	0x6a, 0x65, 0x55, 0x4e, 0x12, 0x4d, 0x4a, 0x7b, 0xdc, 0x96, 0xe8, 0xd8, 0x24, 0x96, 0xcc, 0x9e,
	0x4c, 0x22, 0xf9, 0xbe, 0x25, 0x69, 0xa1, 0x0d, 0x3b, 0x16, 0x93, 0x16, 0xc7, 0x3f, 0xe1, 0x74,
	0xc9, 0xf9, 0x77, 0x8b, 0x6b, 0xca, 0x68, 0x57, 0x92, 0x75, 0xe2, 0x02, 0x46, 0x9a, 0xf3, 0x28,
	0x98, 0x07, 0x97, 0x27, 0x57, 0xcf, 0x16, 0x0f, 0x47, 0x97, 0x9c, 0x67, 0x95, 0x22, 0xde, 0xc2,
	0xb1, 0xe6, 0xdc, 0x7e, 0x73, 0xe8, 0x28, 0x3a, 0x9a, 0x8f, 0x0e, 0x6d, 0x9d, 0x1e, 0xff, 0x09,
	0xe0, 0xac, 0x9b, 0x60, 0x35, 0x17, 0x96, 0xc4, 0x35, 0x84, 0x86, 0x2c, 0x97, 0x46, 0xd2, 0x17,
	0x65, 0x5d, 0x33, 0xeb, 0xbc, 0x0b, 0xc9, 0x7a, 0x6a, 0x36, 0xf0, 0x8a, 0x29, 0x8c, 0xef, 0xd1,
	0xc9, 0x4d, 0x74, 0x34, 0x0f, 0x2e, 0x27, 0x59, 0x0d, 0xc4, 0x6b, 0x18, 0x93, 0x31, 0x6c, 0xa2,
	0x91, 0x8f, 0x3a, 0xed, 0xa2, 0x6e, 0x2a, 0x3a, 0xab, 0xd5, 0xf8, 0x3d, 0x8c, 0x3d, 0xae, 0x52,
	0x6a, 0x7f, 0x50, 0xa7, 0x78, 0x20, 0x62, 0x08, 0xfd, 0xc7, 0x57, 0xb2, 0xd5, 0xff, 0xfa, 0x11,
	0xc7, 0xd9, 0x80, 0x8b, 0xa7, 0x20, 0x3e, 0x13, 0x6e, 0xdd, 0xe6, 0xe3, 0x86, 0xe4, 0x5d, 0xb3,
	0xb4, 0x38, 0x85, 0xe7, 0x03, 0xb6, 0x29, 0x1a, 0xc1, 0xd3, 0x8d, 0xa7, 0x7f, 0x37, 0x83, 0x5a,
	0x18, 0x5f, 0x43, 0xd8, 0x2f, 0x29, 0xde, 0xc0, 0x59, 0xbf, 0xe6, 0xad, 0xe5, 0xc2, 0x1f, 0x09,
	0xb3, 0x03, 0x3e, 0xbe, 0x80, 0xd1, 0x92, 0xf3, 0x2a, 0x5c, 0x73, 0xde, 0x73, 0xb6, 0xf0, 0xea,
	0x6f, 0x00, 0x93, 0x76, 0xe9, 0xe2, 0x06, 0xc2, 0xf6, 0xfb, 0x53, 0x59, 0x48, 0xf1, 0x62, 0x70,
	0x57, 0xfd, 0xab, 0x9f, 0xcd, 0xfe, 0x27, 0x35, 0x55, 0x6e, 0xe1, 0xa4, 0xd7, 0x50, 0xbc, 0xec,
	0xac, 0x87, 0xeb, 0x98, 0xbd, 0x7a, 0x44, 0xad, 0xb3, 0x3e, 0x9c, 0xff, 0x98, 0x22, 0xee, 0x92,
	0xe6, 0xc9, 0x3e, 0x78, 0x57, 0x4f, 0xfc, 0xf3, 0x7c, 0xf7, 0x2f, 0x00, 0x00, 0xff, 0xff, 0x6c,
	0x9d, 0xe4, 0x4a, 0x04, 0x03, 0x00, 0x00,
}