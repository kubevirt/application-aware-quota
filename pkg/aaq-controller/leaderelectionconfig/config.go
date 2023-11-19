package leaderelectionconfig

import (
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DefaultLeaseDuration = 15 * time.Second
	DefaultRenewDeadline = 10 * time.Second
	DefaultRetryPeriod   = 2 * time.Second
)

func DefaultLeaderElectionConfiguration() Configuration {
	return Configuration{
		LeaseDuration: metav1.Duration{Duration: DefaultLeaseDuration},
		RenewDeadline: metav1.Duration{Duration: DefaultRenewDeadline},
		RetryPeriod:   metav1.Duration{Duration: DefaultRetryPeriod},
		ResourceLock:  resourcelock.LeasesResourceLock,
	}
}
