package aaq_evaluator

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/utils/clock"
	"kubevirt.io/client-go/log"
	"sync"
)

// UsageCalculator knows how to evaluate quota usage for a particular app pods
type UsageCalculator interface {
	SetConfiguration(string)
	// Usage returns the resource usage for the specified object
	PodUsageFunc(item runtime.Object, items []runtime.Object, clock clock.Clock) (corev1.ResourceList, error, bool)
}

func NewAaqCalculatorsRegistry(retriesOnMatchFailure int, clock clock.Clock) *AaqCalculatorsRegistry {
	return &AaqCalculatorsRegistry{
		retriesOnMatchFailure: retriesOnMatchFailure,
		clock:                 clock,
		builtInCalculators:    make(map[string]UsageCalculator),
	}
}

type AaqCalculatorsRegistry struct {
	builtInCalculators map[string]UsageCalculator
	// used to track time
	clock                 clock.Clock
	retriesOnMatchFailure int
	mu                    sync.RWMutex // Read-Write Mutex for thread-safety
}

func (aaqe *AaqCalculatorsRegistry) AddBuiltInCalculator(id string, usageCalculator UsageCalculator) *AaqCalculatorsRegistry {
	aaqe.mu.Lock()
	defer aaqe.mu.Unlock()
	aaqe.builtInCalculators[id] = usageCalculator
	return aaqe
}

func (aaqe *AaqCalculatorsRegistry) ReplaceBuiltInCalculatorConfig(id string, config string) *AaqCalculatorsRegistry {
	aaqe.mu.Lock()
	defer aaqe.mu.Unlock()
	_, ok := aaqe.builtInCalculators[id]
	if ok {
		aaqe.builtInCalculators[id].SetConfiguration(config)

	}
	return aaqe
}

func (aaqe *AaqCalculatorsRegistry) Usage(item runtime.Object, items []runtime.Object) (rlToRet corev1.ResourceList, acceptedErr error) {
	aaqe.mu.RLock()
	defer aaqe.mu.RUnlock()

	accepted := false
	for _, calculator := range aaqe.builtInCalculators {
		for retries := 0; retries < aaqe.retriesOnMatchFailure; retries++ {
			rl, err, match := calculator.PodUsageFunc(item, items, aaqe.clock)
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
