package aaq_evaluator

import (
	"context"
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"kubevirt.io/application-aware-quota/pkg/log"
	"kubevirt.io/application-aware-quota/pkg/util/net/grpc"
	sidecar_evaluator "kubevirt.io/application-aware-quota/staging/evaluator-server-com"
	"time"
)

type AaqSocketCalculator struct {
	sidecarSocketPath string
}

func (aaqsc *AaqSocketCalculator) PodUsageFunc(pod *corev1.Pod, podsState []*corev1.Pod) (corev1.ResourceList, error, bool) {
	conn, err := grpc.DialSocketWithTimeout(aaqsc.sidecarSocketPath, 1)
	if err != nil {
		log.Log.Reason(err).Errorf(dialSockErr, aaqsc.sidecarSocketPath)
		return nil, err, false
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	client := sidecar_evaluator.NewPodUsageClient(conn)
	podData, err := json.Marshal(pod)
	if err != nil {
		return nil, err, false
	}
	var podsStateData []*sidecar_evaluator.Pod
	for _, p := range podsState {
		pData, err := json.Marshal(p)
		if err != nil {
			return nil, err, false
		}
		podsStateData = append(podsStateData, &sidecar_evaluator.Pod{pData})
	}
	result, err := client.PodUsageFunc(ctx, &sidecar_evaluator.PodUsageRequest{
		Pod:       &sidecar_evaluator.Pod{podData},
		PodsState: podsStateData,
	})
	if err != nil {
		log.Log.Reason(err).Error(fmt.Sprintf("Failed to call PodUsageFunc with pod %v", pod))
		return nil, err, false
	}
	rl := corev1.ResourceList{}
	if err := json.Unmarshal(result.ResourceList.ResourceListJson, &rl); err != nil {
		return nil, fmt.Errorf("Failed to unmarshal given rl : %s due %v", result.ResourceList.ResourceListJson, err), false
	}
	var resErr error
	if result.Error.Error {
		resErr = fmt.Errorf(result.Error.ErrorMessage)
	}
	return rl, resErr, result.Match
}
