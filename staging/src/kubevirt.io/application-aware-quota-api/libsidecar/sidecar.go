package libsidecar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog"
	"net"
	"os"
	"path/filepath"
	aaqsidecarevaluate "sidecar/evaluator-server-com"
)

const (
	SocketsSharedDirectory = "/var/run/aaq-sockets"
)

type SidecarCalculator interface {
	PodUsageFunc(podToEvaluate *corev1.Pod, existingPods []*corev1.Pod) (corev1.ResourceList, error, bool)
}

func RunServer(scc SidecarCalculator) {
	socketPath, err := getSocketPath()
	if err != nil {
		klog.Errorf("Enviroment error")
		os.Exit(1)
	}
	socket, err := net.Listen("unix", socketPath)
	if err != nil {
		klog.Errorf("Failed to initialized socket on path: %s err: %v", socketPath, err.Error())
		klog.Errorf("Check whether given directory exists and socket name is not already taken by other file")
		os.Exit(1)
	}
	defer os.Remove(socketPath)
	s := Server{scc}
	grpcServer := grpc.NewServer()
	aaqsidecarevaluate.RegisterPodUsageServer(grpcServer, &s)

	if err := grpcServer.Serve(socket); err != nil {
		klog.Fatalf("Failed to serve gRPC server over port 9000: %v", err)
	}
}

func getSocketPath() (string, error) {
	if _, err := os.Stat(SocketsSharedDirectory); err != nil {
		return "", fmt.Errorf("Failed dir %s due %s", SocketsSharedDirectory, err.Error())
	}

	for i := 0; i < 10; i++ {
		socketName := fmt.Sprintf("sidecar-%s.sock", rand.String(4))
		socketPath := filepath.Join(SocketsSharedDirectory, socketName)
		if _, err := os.Stat(socketPath); !errors.Is(err, os.ErrNotExist) {
			klog.Infof("Failed socket %s due %s", socketName, err.Error())
			continue
		}
		return socketPath, nil
	}

	return "", fmt.Errorf("failed generate socket path")
}

type Server struct {
	sidecarCalculator SidecarCalculator
}

func (s *Server) PodUsageFunc(_ context.Context, request *aaqsidecarevaluate.PodUsageRequest) (*aaqsidecarevaluate.PodUsageResponse, error) {
	podToEvaluate := &corev1.Pod{}
	var existingPods []*corev1.Pod
	err := json.Unmarshal(request.Pod.GetPodJson(), podToEvaluate)
	if err != nil {
		return nil, err
	}
	for _, pItem := range request.GetPodsState() {
		currPod := &corev1.Pod{}
		err := json.Unmarshal(pItem.GetPodJson(), currPod)
		if err != nil {
			return nil, err
		}
		existingPods = append(existingPods, currPod)
	}

	rl, err, match := s.sidecarCalculator.PodUsageFunc(podToEvaluate, existingPods)

	rlData, err := json.Marshal(rl)
	if err != nil {
		return nil, err
	}
	podUsageResponse := &aaqsidecarevaluate.PodUsageResponse{
		Error:        &aaqsidecarevaluate.Error{},
		Match:        match,
		ResourceList: &aaqsidecarevaluate.ResourceList{ResourceListJson: rlData},
	}
	if err != nil {
		podUsageResponse.Error.Error = true
		podUsageResponse.Error.ErrorMessage = err.Error()
	}

	return podUsageResponse, nil
}

func (s *Server) HealthCheck(_ context.Context, _ *aaqsidecarevaluate.HealthCheckRequest) (*aaqsidecarevaluate.HealthCheckResponse, error) {
	return &aaqsidecarevaluate.HealthCheckResponse{Healthy: true}, nil
}
