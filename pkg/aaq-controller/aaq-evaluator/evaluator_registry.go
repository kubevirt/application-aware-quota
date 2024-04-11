package aaq_evaluator

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"kubevirt.io/application-aware-quota/pkg/log"
	"kubevirt.io/application-aware-quota/pkg/util"
	pb "kubevirt.io/application-aware-quota/pkg/util/net/generated"
	"kubevirt.io/application-aware-quota/pkg/util/net/grpc"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const dialSockErr = "Failed to Dial socket: %s"

var aaqEvaluatorsRegistry *AaqEvaluatorRegistry
var once sync.Once

type AaqCalculator interface {
	PodUsageFunc(pod *corev1.Pod, podsState []*corev1.Pod) (corev1.ResourceList, error, bool)
}

type Registry interface {
	Add(aaqCalculator AaqCalculator)
	Collect(numberOfRequestedEvaluatorsSidecars uint, timeout time.Duration) error
	Usage(*corev1.Pod, []*corev1.Pod) (corev1.ResourceList, error)
}

type AaqEvaluatorRegistry struct {
	aaqCalculators        []AaqCalculator
	socketSharedDirectory string
	// used to track time
	retriesOnMatchFailure int
}

func newAaqEvaluatorsRegistry(retriesOnMatchFailure int, socketSharedDirectory string) *AaqEvaluatorRegistry {
	return &AaqEvaluatorRegistry{
		retriesOnMatchFailure: retriesOnMatchFailure,
		socketSharedDirectory: socketSharedDirectory,
	}
}

func GetAaqEvaluatorsRegistry() *AaqEvaluatorRegistry {
	once.Do(func() {
		aaqEvaluatorsRegistry = newAaqEvaluatorsRegistry(10, util.SocketsSharedDirectory)
	})
	return aaqEvaluatorsRegistry
}

func (aaqe *AaqEvaluatorRegistry) Collect(numberOfRequestedEvaluatorsSidecars uint, timeout time.Duration) error {
	socketsPaths, err := aaqe.collectSidecarSockets(numberOfRequestedEvaluatorsSidecars, timeout)
	if err != nil {
		return err
	}
	for _, socketPath := range socketsPaths {
		aaqe.aaqCalculators = append(aaqe.aaqCalculators, &AaqSocketCalculator{socketPath})
	}
	log.Log.Info("Collected all requested evaluators sidecars sockets")
	return nil
}

func (aaqe *AaqEvaluatorRegistry) Add(aaqCalculator AaqCalculator) {
	aaqe.aaqCalculators = append(aaqe.aaqCalculators, aaqCalculator)
}

func (aaqe *AaqEvaluatorRegistry) collectSidecarSockets(numberOfRequestedEvaluatorsSidecars uint, timeout time.Duration) ([]string, error) {
	var sidecarSockets []string
	processedSockets := make(map[string]bool)

	timeoutCh := time.After(timeout)

	for uint(len(processedSockets)) < numberOfRequestedEvaluatorsSidecars {
		sockets, err := os.ReadDir(aaqe.socketSharedDirectory)
		if err != nil {
			return nil, err
		}
		for _, socket := range sockets {
			select {
			case <-timeoutCh:
				return nil, fmt.Errorf("Failed to collect all expected evaluators sidecars sockets within given timeout")
			default:
				if _, processed := processedSockets[socket.Name()]; processed {
					continue
				}

				callBackClient, notReady, err := processSideCarSocket(filepath.Join(aaqe.socketSharedDirectory, socket.Name()))
				if notReady {
					log.Log.Info("Sidecar server might not be ready yet, retrying in the next iteration")
					continue
				} else if err != nil {
					log.Log.Reason(err).Infof("Failed to process sidecar socket: %s", socket.Name())
					return nil, err
				}
				sidecarSockets = append(sidecarSockets, callBackClient)
				processedSockets[socket.Name()] = true
			}
		}
		time.Sleep(time.Second)
	}
	return sidecarSockets, nil
}

func processSideCarSocket(socketPath string) (string, bool, error) {
	conn, err := grpc.DialSocketWithTimeout(socketPath, 1)
	if err != nil {
		log.Log.Reason(err).Infof(dialSockErr, socketPath)
		return "", true, nil
	}
	defer conn.Close()

	infoClient := pb.NewPodUsageClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	health, err := infoClient.HealthCheck(ctx, &pb.HealthCheckRequest{})
	if err != nil || health == nil || !health.Healthy {
		if err == nil {
			err = fmt.Errorf("HealthCheck failed with following socket: %v", socketPath)
		}
		return "", false, err
	}
	return socketPath, false, nil
}

func (aaqe *AaqEvaluatorRegistry) Usage(pod *corev1.Pod, podsState []*corev1.Pod) (rlToRet corev1.ResourceList, acceptedErr error) {
	accepted := false
	for _, calculator := range aaqe.aaqCalculators {
		for retries := 0; retries < aaqe.retriesOnMatchFailure; retries++ {
			rl, err, match := calculator.PodUsageFunc(pod, podsState)
			if !match && err == nil {
				break
			} else if err == nil {
				accepted = true
				rlToRet = quota.Add(rlToRet, rl)
				break
			} else {
				log.Log.Infof(fmt.Sprintf("Retries: %v Error: %v ", retries, err))
			}
		}
	}
	if !accepted {
		acceptedErr = fmt.Errorf("pod didn't match any usageFunc")
	}
	return rlToRet, acceptedErr
}
